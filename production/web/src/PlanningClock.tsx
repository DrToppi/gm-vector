import {createContext,useContext} from 'react';
import {currentLocale,tr} from './i18n';
import {COMPATIBILITY_ZONE,planningToday,planningTimestamp,supportsPlanningZone} from './planningTime';

const PlanningZone=createContext(COMPATIBILITY_ZONE);
export function PlanningClockProvider({zone,children}:{zone:string;children:React.ReactNode}){
 return <PlanningZone.Provider value={zone}>{!supportsPlanningZone(zone)&&<div className="error" role="alert" data-planning-zone-error>{tr('clock.unsupported',{zone:zone||tr('clock.missing')})}</div>}{children}</PlanningZone.Provider>;
}
export function usePlanningClock(){
 const zone=useContext(PlanningZone),ready=supportsPlanningZone(zone);
 return {zone,ready,today:()=>ready?planningToday(zone):'',timestamp:(value:string)=>ready?planningTimestamp(value,zone,currentLocale()):value+' (UTC)'};
}
