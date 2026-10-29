import type {Link,Project} from './types';
export type DisplayLink=Link&{index:number};
// Project hidden endpoints onto visible ancestors; never mutate the saved edge.
export function mapLinks(project:Project,expanded:DisplayLink[],visible:Set<string>,selected:string,all:boolean):DisplayLink[] {
 const parents=new Map(project.tasks.map(t=>[t.id,t.parent]));
 const representative=(id:string)=>{const visited=new Set<string>();while(id&&!visible.has(id)&&!visited.has(id)){visited.add(id);id=parents.get(id)||''}return visible.has(id)?id:''};
 const byIndex=new Map<number,DisplayLink[]>();
 for(const edge of expanded){const group=byIndex.get(edge.index)||[];group.push(edge);byIndex.set(edge.index,group)}
 const result:DisplayLink[]=[],seen=new Set<string>();
 project.links.forEach((original,index)=>{
  // Selecting an explicit container endpoint shows its original relationship.
  const edges=!all&&(original.from===selected||original.to===selected)?[{...original,index}]:byIndex.get(index)||[{...original,index}];
  for(const edge of edges){const from=representative(edge.from),to=representative(edge.to);
   if(!from||!to||from===to)continue;
   if(!all&&from!==selected&&to!==selected)continue;
   const key=JSON.stringify([index,from,to]);if(seen.has(key))continue;seen.add(key);
   result.push({...edge,from,to});
  }
 });
 return result;
}
