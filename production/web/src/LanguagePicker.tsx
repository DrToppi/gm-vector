import {useState} from 'react';
import {useTranslation} from 'react-i18next';
import {changeLocale,currentLocale,isLocale,localeNames,locales,tr} from './i18n';
import {sessionScope} from './api';
export function LanguagePicker({authenticated=false}:{authenticated?:boolean}){
 useTranslation();const [busy,setBusy]=useState(false),[error,setError]=useState('');
 return <div className="language-picker"><label><span aria-hidden="true">◎ </span><span className="sr-only">{tr('language')}</span><select aria-label={tr('language')} disabled={busy} value={currentLocale()} onChange={async e=>{const next=e.target.value;if(!isLocale(next))return;const scope=sessionScope();setBusy(true);setError('');try{if(authenticated){await scope.api('/auth/preferences','PUT',{locale:next});if(!scope.current())return}await changeLocale(next)}catch{if(scope.current())setError(tr('languageSaveError'))}finally{setBusy(false)}}}>{locales.map(locale=><option key={locale} value={locale} lang={locale}>{localeNames[locale]}</option>)}</select></label>{error&&<small role="alert">{error}</small>}</div>
}
