import i18n from 'i18next';
import {initReactI18next} from 'react-i18next';
import en from './locales/en.json';
import ru from './locales/ru.json';
import de from './locales/de.json';
import fr from './locales/fr.json';
import es from './locales/es.json';
import ptBR from './locales/pt-BR.json';
import zhHans from './locales/zh-Hans.json';
export const locales=['en','ru','de','fr','es','pt-BR','zh-Hans'] as const;
export type Locale=typeof locales[number];
export const localeNames:Record<Locale,string>={en:'English',ru:'Русский',de:'Deutsch',fr:'Français',es:'Español','pt-BR':'Português (Brasil)','zh-Hans':'简体中文'};
export function isLocale(value:unknown):value is Locale{return typeof value==='string'&&(locales as readonly string[]).includes(value)}
const storageKey='gm-vector.locale';
function initialLocale():Locale{try{const saved=localStorage.getItem(storageKey);if(isLocale(saved))return saved}catch{}return 'en'}
void i18n.use(initReactI18next).init({resources:{en:{translation:en},ru:{translation:ru},de:{translation:de},fr:{translation:fr},es:{translation:es},'pt-BR':{translation:ptBR},'zh-Hans':{translation:zhHans}},lng:initialLocale(),fallbackLng:'en',supportedLngs:[...locales],load:'currentOnly',initAsync:false,interpolation:{escapeValue:false},returnNull:false});
export function tr(key:string,values?:Record<string,unknown>):string{return String(i18n.t(key,values))}
export function currentLocale():Locale{return isLocale(i18n.resolvedLanguage)?i18n.resolvedLanguage:'en'}
export async function changeLocale(locale:Locale){await i18n.changeLanguage(locale);if(typeof document!=='undefined')document.documentElement.lang=locale;try{localStorage.setItem(storageKey,locale)}catch{}}
if(typeof document!=='undefined')document.documentElement.lang=currentLocale();
export default i18n;
