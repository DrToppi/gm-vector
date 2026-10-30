import type {Project} from './types';
export type PortableProject={format:'gm-vector-project';schema_version:1;metadata:{planning_timezone:string;generated_at:string};project:Project};
export type TransferDocument={kind:'legacy';project:Project}|{kind:'portable';document:PortableProject};
export type TimezoneAcknowledgement={source:string;target:string};
export type ImportPreview={project:Project;message:string;source_planning_timezone?:string;target_planning_timezone?:string;timezone_acknowledgement_required?:boolean};
export function detectProjectDocument(value:unknown):TransferDocument{
 if(!value||typeof value!=='object'||Array.isArray(value))throw Error('portable.invalidFile');
 const data=value as Record<string,unknown>;
 if('format'in data){
  if(data.format!=='gm-vector-project'||data.schema_version!==1||!data.metadata||typeof data.metadata!=='object'||!data.project||typeof data.project!=='object')throw Error('portable.invalidFile');
  return {kind:'portable',document:value as PortableProject};
 }
 return {kind:'legacy',project:value as Project};
}
export function projectImportRequest(value:TransferDocument,preview:boolean,acknowledgement?:TimezoneAcknowledgement){
 return value.kind==='portable'?{path:'/import/portable',body:{document:value.document,preview,...(acknowledgement?{timezone_acknowledgement:acknowledgement}:{})}}:{path:'/import',body:{project:value.project,preview}};
}
