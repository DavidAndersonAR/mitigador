import { createI18n } from 'vue-i18n';
import ptBR from './pt-BR.json';
import enUS from './en-US.json';

export const i18n = createI18n({
  legacy: false,
  locale: localStorage.getItem('mitigador.locale') ?? 'pt-BR',
  fallbackLocale: 'pt-BR',
  messages: { 'pt-BR': ptBR, 'en-US': enUS },
});

export function setLocale(loc: 'pt-BR' | 'en-US') {
  (i18n.global.locale as { value: string }).value = loc;
  localStorage.setItem('mitigador.locale', loc);
}
