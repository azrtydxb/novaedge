import{j as a}from"./ui-weZN0jFY.js";import{a as s}from"./vendor-Dk2pv1e-.js";import{u as Y,j as C,k as I,R as _,l as k,B as v}from"./index-C8oZIp8K.js";import{e as q,y as G,z as H,A as J,n as K}from"./table-DKkOcbiQ.js";import{D as W}from"./DataTable-BaOzkaFK.js";import{T as Q,P as X}from"./trash-2-DIeS4fQH.js";import"./query-tSZhJNjp.js";import"./charts-B_DxHS0G.js";import"./input-D1xKGq3n.js";const Z=`apiVersion: novaedge.io/v1alpha1
kind: ProxyPolicy
metadata:
  name: my-policy
  namespace: default
spec:
  targetRef:
    group: novaedge.io
    kind: ProxyRoute
    name: my-route
  rateLimit:
    requestsPerSecond: 100
    burst: 50
  cors:
    allowOrigins:
      - "*"
    allowMethods:
      - GET
      - POST
      - PUT
      - DELETE
    allowHeaders:
      - Content-Type
      - Authorization
    maxAge: 86400
`;function re(){const{namespace:j,readOnly:i}=Y(),{data:L=[],isLoading:S,error:d}=q(j),u=G(),p=H(),y=J(),g=K("policies"),[n,f]=s.useState(new Set),[T,r]=s.useState(!1),[o,h]=s.useState("create"),[b,P]=s.useState(),[w,D]=s.useState(!1),[c,A]=s.useState(null),[O,x]=s.useState(!1),R=e=>{const t=[];return e.spec?.rateLimit&&t.push("Rate Limit"),e.spec?.cors&&t.push("CORS"),e.spec?.ipFilter&&t.push("IP Filter"),e.spec?.jwt&&t.push("JWT"),t},N=[{key:"name",header:"Name",accessor:e=>e.metadata?.name??"-",sortable:!0},{key:"namespace",header:"Namespace",accessor:e=>a.jsx(v,{variant:"outline",children:e.metadata?.namespace??"-"}),sortable:!0},{key:"target",header:"Target",accessor:e=>{const t=e.spec?.targetRef;return t?`${t.kind}/${t.name}`:"-"}},{key:"types",header:"Policy Types",accessor:e=>{const t=R(e);return t.length>0?a.jsx("div",{className:"flex flex-wrap gap-1",children:t.map(l=>a.jsx(v,{variant:"secondary",className:"text-xs",children:l},l))}):"-"}},{key:"rateLimit",header:"Rate Limit",accessor:e=>e.spec?.rateLimit?`${e.spec.rateLimit.requestsPerSecond} req/s`:"-"},{key:"age",header:"Age",accessor:e=>e.metadata?.creationTimestamp?I(e.metadata.creationTimestamp):"-",sortable:!0}],E=()=>{P(void 0),h("create"),r(!0)},m=e=>{P(e),h(i?"view":"edit"),r(!0)},B=e=>{A(e),D(!0)},$=()=>{c&&y.mutate({namespace:c.metadata?.namespace??"",name:c.metadata?.name??""})},z=()=>{x(!0)},F=()=>{const e=Array.from(n).map(t=>{const[l,V]=t.split("/");return{namespace:l,name:V}});g.mutate(e,{onSuccess:()=>f(new Set)})},M=async e=>{o==="create"?await u.mutateAsync(e):await p.mutateAsync({namespace:e.metadata?.namespace??"",name:e.metadata?.name??"",policy:e})},U=e=>`${e.metadata?.namespace}/${e.metadata?.name}`;return d?a.jsxs("div",{className:"text-center py-12 text-destructive",children:["Failed to load policies: ",d.message]}):a.jsxs("div",{className:"space-y-4",children:[a.jsxs("div",{className:"flex items-center justify-between",children:[a.jsx("div",{className:"flex items-center gap-2",children:!i&&n.size>0&&a.jsxs(C,{variant:"destructive",size:"sm",onClick:z,children:[a.jsx(Q,{className:"h-4 w-4 mr-2"}),"Delete (",n.size,")"]})}),!i&&a.jsxs(C,{onClick:E,children:[a.jsx(X,{className:"h-4 w-4 mr-2"}),"Create Policy"]})]}),a.jsx(W,{data:L,columns:N,getRowKey:U,selectable:!i,selectedRows:n,onSelectionChange:f,onRowClick:m,isLoading:S,emptyMessage:"No policies found",searchPlaceholder:"Search policies...",searchFilter:(e,t)=>e.metadata?.name?.toLowerCase().includes(t)||e.metadata?.namespace?.toLowerCase().includes(t)||e.spec?.targetRef?.name?.toLowerCase().includes(t)||!1,actions:e=>i?[{label:"View",onClick:()=>m(e)}]:[{label:"Edit",onClick:()=>m(e)},{label:"Delete",onClick:()=>B(e),variant:"destructive"}]}),a.jsx(_,{open:T,onOpenChange:r,title:o==="create"?"Create Policy":o==="edit"?"Edit Policy":"View Policy",description:o==="create"?"Define a new policy configuration":void 0,mode:o,resource:b,onSubmit:M,isLoading:u.isPending||p.isPending,readOnly:i,defaultYaml:Z}),a.jsx(k,{open:w,onOpenChange:D,title:"Delete Policy",description:`Are you sure you want to delete policy "${c?.metadata?.name}"? This action cannot be undone.`,confirmLabel:"Delete",variant:"destructive",onConfirm:$,isLoading:y.isPending}),a.jsx(k,{open:O,onOpenChange:x,title:"Delete Selected Policies",description:`Are you sure you want to delete ${n.size} policy(ies)? This action cannot be undone.`,confirmLabel:"Delete All",variant:"destructive",onConfirm:F,isLoading:g.isPending})]})}export{re as default};
