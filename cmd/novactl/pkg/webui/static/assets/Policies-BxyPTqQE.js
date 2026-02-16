import{j as a}from"./ui-C7EZ6zpn.js";import{a as s}from"./vendor-CwhvS1Xk.js";import{u as Y,k as C,l as I,R as _,m as k,e as v}from"./index-D2NKZ5iG.js";import{e as q,A as G,B as H,C as J,p as K}from"./hooks-LIr8xFai.js";import{D as W}from"./DataTable-Nknefu9o.js";import{T as Q,P as X}from"./trash-2-WueFybTX.js";import"./query-B9ZAROpx.js";import"./charts-I6YY2jED.js";import"./table-DQF6FC_p.js";import"./chevron-up-D2Rliuq6.js";import"./input-CYP15ZsC.js";import"./search-BE7d1k6s.js";const Z=`apiVersion: novaedge.io/v1alpha1
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
`;function pe(){const{namespace:L,readOnly:i}=Y(),{data:S=[],isLoading:j,error:d}=q(L),p=G(),u=H(),g=J(),y=K("policies"),[n,f]=s.useState(new Set),[T,l]=s.useState(!1),[o,h]=s.useState("create"),[b,P]=s.useState(),[w,D]=s.useState(!1),[c,A]=s.useState(null),[O,x]=s.useState(!1),R=e=>{const t=[];return e.spec?.rateLimit&&t.push("Rate Limit"),e.spec?.cors&&t.push("CORS"),e.spec?.ipFilter&&t.push("IP Filter"),e.spec?.jwt&&t.push("JWT"),t},N=[{key:"name",header:"Name",accessor:e=>e.metadata?.name??"-",sortable:!0},{key:"namespace",header:"Namespace",accessor:e=>a.jsx(v,{variant:"outline",children:e.metadata?.namespace??"-"}),sortable:!0},{key:"target",header:"Target",accessor:e=>{const t=e.spec?.targetRef;return t?`${t.kind}/${t.name}`:"-"}},{key:"types",header:"Policy Types",accessor:e=>{const t=R(e);return t.length>0?a.jsx("div",{className:"flex flex-wrap gap-1",children:t.map(r=>a.jsx(v,{variant:"secondary",className:"text-xs",children:r},r))}):"-"}},{key:"rateLimit",header:"Rate Limit",accessor:e=>e.spec?.rateLimit?`${e.spec.rateLimit.requestsPerSecond} req/s`:"-"},{key:"age",header:"Age",accessor:e=>e.metadata?.creationTimestamp?I(e.metadata.creationTimestamp):"-",sortable:!0}],E=()=>{P(void 0),h("create"),l(!0)},m=e=>{P(e),h(i?"view":"edit"),l(!0)},B=e=>{A(e),D(!0)},$=()=>{c&&g.mutate({namespace:c.metadata?.namespace??"",name:c.metadata?.name??""})},z=()=>{x(!0)},F=()=>{const e=Array.from(n).map(t=>{const[r,V]=t.split("/");return{namespace:r,name:V}});y.mutate(e,{onSuccess:()=>f(new Set)})},M=async e=>{o==="create"?await p.mutateAsync(e):await u.mutateAsync({namespace:e.metadata?.namespace??"",name:e.metadata?.name??"",policy:e})},U=e=>`${e.metadata?.namespace}/${e.metadata?.name}`;return d?a.jsxs("div",{className:"text-center py-12 text-destructive",children:["Failed to load policies: ",d.message]}):a.jsxs("div",{className:"space-y-4",children:[a.jsxs("div",{className:"flex items-center justify-between",children:[a.jsx("div",{className:"flex items-center gap-2",children:!i&&n.size>0&&a.jsxs(C,{variant:"destructive",size:"sm",onClick:z,children:[a.jsx(Q,{className:"h-4 w-4 mr-2"}),"Delete (",n.size,")"]})}),!i&&a.jsxs(C,{onClick:E,children:[a.jsx(X,{className:"h-4 w-4 mr-2"}),"Create Policy"]})]}),a.jsx(W,{data:S,columns:N,getRowKey:U,selectable:!i,selectedRows:n,onSelectionChange:f,onRowClick:m,isLoading:j,emptyMessage:"No policies found",searchPlaceholder:"Search policies...",searchFilter:(e,t)=>e.metadata?.name?.toLowerCase().includes(t)||e.metadata?.namespace?.toLowerCase().includes(t)||e.spec?.targetRef?.name?.toLowerCase().includes(t)||!1,actions:e=>i?[{label:"View",onClick:()=>m(e)}]:[{label:"Edit",onClick:()=>m(e)},{label:"Delete",onClick:()=>B(e),variant:"destructive"}]}),a.jsx(_,{open:T,onOpenChange:l,title:o==="create"?"Create Policy":o==="edit"?"Edit Policy":"View Policy",description:o==="create"?"Define a new policy configuration":void 0,mode:o,resource:b,onSubmit:M,isLoading:p.isPending||u.isPending,readOnly:i,defaultYaml:Z}),a.jsx(k,{open:w,onOpenChange:D,title:"Delete Policy",description:`Are you sure you want to delete policy "${c?.metadata?.name}"? This action cannot be undone.`,confirmLabel:"Delete",variant:"destructive",onConfirm:$,isLoading:g.isPending}),a.jsx(k,{open:O,onOpenChange:x,title:"Delete Selected Policies",description:`Are you sure you want to delete ${n.size} policy(ies)? This action cannot be undone.`,confirmLabel:"Delete All",variant:"destructive",onConfirm:F,isLoading:y.isPending})]})}export{pe as default};
