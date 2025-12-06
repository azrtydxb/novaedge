import{j as s}from"./ui-C3LS0mvo.js";import{r as n}from"./vendor-B9G4DQgX.js";import{u as J,k as w,l as K,R as W,m as T,B as A}from"./index-CCANUtDI.js";import{e as Q,y as X,z as Z,A as ee,n as te}from"./table-D_TGSWfE.js";import{D as ae}from"./DataTable-BNufweeu.js";import{T as se,P as ie}from"./trash-2-3tnh-ezI.js";import"./query-DSINdlgF.js";import"./charts-Cd4C1skx.js";const ne=`apiVersion: novaedge.io/v1alpha1
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
`;function ge(){var L;const{namespace:O,readOnly:o}=J(),{data:R=[],isLoading:N,error:g}=Q(O),y=X(),f=Z(),h=ee(),P=te("policies"),[r,x]=n.useState(new Set),[E,u]=n.useState(!1),[l,D]=n.useState("create"),[B,C]=n.useState(),[$,k]=n.useState(!1),[c,z]=n.useState(null),[F,v]=n.useState(!1),M=e=>{var a,i,m,d;const t=[];return(a=e.spec)!=null&&a.rateLimit&&t.push("Rate Limit"),(i=e.spec)!=null&&i.cors&&t.push("CORS"),(m=e.spec)!=null&&m.ipFilter&&t.push("IP Filter"),(d=e.spec)!=null&&d.jwt&&t.push("JWT"),t},U=[{key:"name",header:"Name",accessor:e=>{var t;return((t=e.metadata)==null?void 0:t.name)??"-"},sortable:!0},{key:"namespace",header:"Namespace",accessor:e=>{var t;return s.jsx(A,{variant:"outline",children:((t=e.metadata)==null?void 0:t.namespace)??"-"})},sortable:!0},{key:"target",header:"Target",accessor:e=>{var a;const t=(a=e.spec)==null?void 0:a.targetRef;return t?`${t.kind}/${t.name}`:"-"}},{key:"types",header:"Policy Types",accessor:e=>{const t=M(e);return t.length>0?s.jsx("div",{className:"flex flex-wrap gap-1",children:t.map(a=>s.jsx(A,{variant:"secondary",className:"text-xs",children:a},a))}):"-"}},{key:"rateLimit",header:"Rate Limit",accessor:e=>{var t;return(t=e.spec)!=null&&t.rateLimit?`${e.spec.rateLimit.requestsPerSecond} req/s`:"-"}},{key:"age",header:"Age",accessor:e=>{var t;return(t=e.metadata)!=null&&t.creationTimestamp?K(e.metadata.creationTimestamp):"-"},sortable:!0}],V=()=>{C(void 0),D("create"),u(!0)},p=e=>{C(e),D(o?"view":"edit"),u(!0)},Y=e=>{z(e),k(!0)},I=()=>{var e,t;c&&h.mutate({namespace:((e=c.metadata)==null?void 0:e.namespace)??"",name:((t=c.metadata)==null?void 0:t.name)??""})},_=()=>{v(!0)},q=()=>{const e=Array.from(r).map(t=>{const[a,i]=t.split("/");return{namespace:a,name:i}});P.mutate(e,{onSuccess:()=>x(new Set)})},G=async e=>{var t,a;l==="create"?await y.mutateAsync(e):await f.mutateAsync({namespace:((t=e.metadata)==null?void 0:t.namespace)??"",name:((a=e.metadata)==null?void 0:a.name)??"",policy:e})},H=e=>{var t,a;return`${(t=e.metadata)==null?void 0:t.namespace}/${(a=e.metadata)==null?void 0:a.name}`};return g?s.jsxs("div",{className:"text-center py-12 text-destructive",children:["Failed to load policies: ",g.message]}):s.jsxs("div",{className:"space-y-4",children:[s.jsxs("div",{className:"flex items-center justify-between",children:[s.jsx("div",{className:"flex items-center gap-2",children:!o&&r.size>0&&s.jsxs(w,{variant:"destructive",size:"sm",onClick:_,children:[s.jsx(se,{className:"h-4 w-4 mr-2"}),"Delete (",r.size,")"]})}),!o&&s.jsxs(w,{onClick:V,children:[s.jsx(ie,{className:"h-4 w-4 mr-2"}),"Create Policy"]})]}),s.jsx(ae,{data:R,columns:U,getRowKey:H,selectable:!o,selectedRows:r,onSelectionChange:x,onRowClick:p,isLoading:N,emptyMessage:"No policies found",searchPlaceholder:"Search policies...",searchFilter:(e,t)=>{var a,i,m,d,S,j,b;return((i=(a=e.metadata)==null?void 0:a.name)==null?void 0:i.toLowerCase().includes(t))||((d=(m=e.metadata)==null?void 0:m.namespace)==null?void 0:d.toLowerCase().includes(t))||((b=(j=(S=e.spec)==null?void 0:S.targetRef)==null?void 0:j.name)==null?void 0:b.toLowerCase().includes(t))||!1},actions:e=>o?[{label:"View",onClick:()=>p(e)}]:[{label:"Edit",onClick:()=>p(e)},{label:"Delete",onClick:()=>Y(e),variant:"destructive"}]}),s.jsx(W,{open:E,onOpenChange:u,title:l==="create"?"Create Policy":l==="edit"?"Edit Policy":"View Policy",description:l==="create"?"Define a new policy configuration":void 0,mode:l,resource:B,onSubmit:G,isLoading:y.isPending||f.isPending,readOnly:o,defaultYaml:ne}),s.jsx(T,{open:$,onOpenChange:k,title:"Delete Policy",description:`Are you sure you want to delete policy "${(L=c==null?void 0:c.metadata)==null?void 0:L.name}"? This action cannot be undone.`,confirmLabel:"Delete",variant:"destructive",onConfirm:I,isLoading:h.isPending}),s.jsx(T,{open:F,onOpenChange:v,title:"Delete Selected Policies",description:`Are you sure you want to delete ${r.size} policy(ies)? This action cannot be undone.`,confirmLabel:"Delete All",variant:"destructive",onConfirm:q,isLoading:P.isPending})]})}export{ge as default};
