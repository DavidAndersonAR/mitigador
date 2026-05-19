package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/mitigador/mitigador/internal/config"
	pg "github.com/mitigador/mitigador/internal/storage/postgres"
	"github.com/mitigador/mitigador/internal/user"
)

func newUserCmd(configPath *string) *cobra.Command {
	c := &cobra.Command{
		Use:   "user",
		Short: "Manage dashboard users",
	}
	c.AddCommand(
		newUserCreateCmd(configPath),
		newUserListCmd(configPath),
		newUserPasswdCmd(configPath),
		newUserDeleteCmd(configPath),
	)
	return c
}

// openStore loads config, runs migrations, and returns a user.Store.
// The returned cleanup func must be called (deferred) to close the pool.
func openStore(configPath string) (*user.Store, func(), error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := pg.Migrate(cfg.Postgres.DSN); err != nil {
		return nil, nil, fmt.Errorf("migrate: %w", err)
	}
	pool, err := pg.NewPool(ctx, cfg.Postgres.DSN, 4, 1)
	if err != nil {
		return nil, nil, err
	}
	return user.NewStore(pool), pool.Close, nil
}

// readPasswordTTY reads a password from the TTY without echo.
// The prompt is printed to stderr so it does not pollute stdout output.
func readPasswordTTY(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	bytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr) // newline after the silent input
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return string(bytes), nil
}

func newUserCreateCmd(configPath *string) *cobra.Command {
	var email string
	c := &cobra.Command{
		Use:   "create <username>",
		Short: "Create a new user (prompts for password at TTY)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := args[0]
			p1, err := readPasswordTTY("Password: ")
			if err != nil {
				return err
			}
			if len(p1) < 12 {
				return fmt.Errorf("password must be at least 12 characters")
			}
			p2, err := readPasswordTTY("Confirm password: ")
			if err != nil {
				return err
			}
			if p1 != p2 {
				return errors.New("passwords do not match")
			}
			store, closeFn, err := openStore(*configPath)
			if err != nil {
				return err
			}
			defer closeFn()
			u, err := store.Create(cmd.Context(), username, email, p1)
			if errors.Is(err, user.ErrAlreadyExists) {
				return fmt.Errorf("user %q already exists", username)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "created user %q (id=%d)\n", u.Username, u.ID)
			return nil
		},
	}
	c.Flags().StringVar(&email, "email", "", "user's email (optional)")
	return c
}

func newUserListCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all dashboard users",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, closeFn, err := openStore(*configPath)
			if err != nil {
				return err
			}
			defer closeFn()
			users, err := store.List(cmd.Context())
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "USERNAME\tEMAIL\tCREATED\tLAST_LOGIN")
			for _, u := range users {
				last := "never"
				if u.LastLogin != nil {
					last = u.LastLogin.UTC().Format(time.RFC3339)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					u.Username, u.Email,
					u.CreatedAt.UTC().Format(time.RFC3339),
					last,
				)
			}
			return w.Flush()
		},
	}
}

func newUserPasswdCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "passwd <username>",
		Short: "Rotate a user's password (prompts at TTY)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p1, err := readPasswordTTY("New password: ")
			if err != nil {
				return err
			}
			if len(p1) < 12 {
				return fmt.Errorf("password must be at least 12 characters")
			}
			p2, err := readPasswordTTY("Confirm new password: ")
			if err != nil {
				return err
			}
			if p1 != p2 {
				return errors.New("passwords do not match")
			}
			store, closeFn, err := openStore(*configPath)
			if err != nil {
				return err
			}
			defer closeFn()
			if err := store.UpdatePassword(cmd.Context(), args[0], p1); err != nil {
				if errors.Is(err, user.ErrNotFound) {
					return fmt.Errorf("user %q not found", args[0])
				}
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "password updated for %q\n", args[0])
			return nil
		},
	}
}

func newUserDeleteCmd(configPath *string) *cobra.Command {
	var assumeYes bool
	c := &cobra.Command{
		Use:   "delete <username>",
		Short: "Delete a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !assumeYes {
				fmt.Fprintf(os.Stderr, "Delete user %q? [y/N]: ", args[0])
				reader := bufio.NewReader(os.Stdin)
				line, _ := reader.ReadString('\n')
				line = strings.ToLower(strings.TrimSpace(line))
				if line != "y" && line != "yes" {
					return errors.New("aborted")
				}
			}
			store, closeFn, err := openStore(*configPath)
			if err != nil {
				return err
			}
			defer closeFn()
			if err := store.Delete(cmd.Context(), args[0]); err != nil {
				if errors.Is(err, user.ErrNotFound) {
					return fmt.Errorf("user %q not found", args[0])
				}
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "deleted user %q\n", args[0])
			return nil
		},
	}
	c.Flags().BoolVar(&assumeYes, "yes", false, "skip the confirmation prompt")
	return c
}
