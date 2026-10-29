// Node regression tests use the actual i18next engine and application catalogs.
const i18n = require('../web/node_modules/i18next').createInstance();
i18n.init({lng:'ru',fallbackLng:'en',initAsync:false,interpolation:{escapeValue:false},resources:{ru:{translation:require('../web/src/locales/ru.json')},en:{translation:require('../web/src/locales/en.json')}}});
module.exports={tr:(key,values)=>String(i18n.t(key,values)),currentLocale:()=>i18n.resolvedLanguage,default:i18n};
