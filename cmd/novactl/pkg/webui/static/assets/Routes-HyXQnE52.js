import{j as t}from"./ui-weZN0jFY.js";import{a as s}from"./vendor-Dk2pv1e-.js";import{u as U,j as k,k as V,R as Y,l as C,B as _}from"./index-C8oZIp8K.js";import{b as G,o as H,p as K,q as I,n as J}from"./table-DKkOcbiQ.js";import{D as Q}from"./DataTable-BaOzkaFK.js";import{T as W,P as X}from"./trash-2-DIeS4fQH.js";import"./query-tSZhJNjp.js";import"./charts-B_DxHS0G.js";import"./input-D1xKGq3n.js";const Z=`apiVersion: novaedge.io/v1alpha1
kind: ProxyRoute
metadata:
  name: my-route
  namespace: default
spec:
  parentRefs:
    - name: my-gateway
      namespace: default
  hostnames:
    - example.com
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /api
      backendRefs:
        - name: my-backend
          namespace: default
          weight: 100
`;function re(){const{namespace:b,readOnly:n}=U(),{data:v=[],isLoading:j,error:u}=G(b),d=H(),p=K(),h=I(),f=J("routes"),[o,g]=s.useState(new Set),[w,l]=s.useState(!1),[c,R]=s.useState("create"),[S,D]=s.useState(),[A,y]=s.useState(!1),[r,L]=s.useState(null),[O,x]=s.useState(!1),T=[{key:"name",header:"Name",accessor:e=>e.metadata?.name??"-",sortable:!0},{key:"namespace",header:"Namespace",accessor:e=>t.jsx(_,{variant:"outline",children:e.metadata?.namespace??"-"}),sortable:!0},{key:"hostnames",header:"Hostnames",accessor:e=>{const a=e.spec?.hostnames??[];return a.length>0?a.join(", "):"*"}},{key:"parentRef",header:"Gateway",accessor:e=>{const a=e.spec?.parentRefs??[];return a.length>0?a.map(i=>i.name).join(", "):"-"}},{key:"rules",header:"Rules",accessor:e=>e.spec?.rules?.length??0},{key:"age",header:"Age",accessor:e=>e.metadata?.creationTimestamp?V(e.metadata.creationTimestamp):"-",sortable:!0}],P=()=>{D(void 0),R("create"),l(!0)},m=e=>{D(e),R(n?"view":"edit"),l(!0)},N=e=>{L(e),y(!0)},B=()=>{r&&h.mutate({namespace:r.metadata?.namespace??"",name:r.metadata?.name??""})},E=()=>{x(!0)},z=()=>{const e=Array.from(o).map(a=>{const[i,F]=a.split("/");return{namespace:i,name:F}});f.mutate(e,{onSuccess:()=>g(new Set)})},M=async e=>{c==="create"?await d.mutateAsync(e):await p.mutateAsync({namespace:e.metadata?.namespace??"",name:e.metadata?.name??"",route:e})},$=e=>`${e.metadata?.namespace}/${e.metadata?.name}`;return u?t.jsxs("div",{className:"text-center py-12 text-destructive",children:["Failed to load routes: ",u.message]}):t.jsxs("div",{className:"space-y-4",children:[t.jsxs("div",{className:"flex items-center justify-between",children:[t.jsx("div",{className:"flex items-center gap-2",children:!n&&o.size>0&&t.jsxs(k,{variant:"destructive",size:"sm",onClick:E,children:[t.jsx(W,{className:"h-4 w-4 mr-2"}),"Delete (",o.size,")"]})}),!n&&t.jsxs(k,{onClick:P,children:[t.jsx(X,{className:"h-4 w-4 mr-2"}),"Create Route"]})]}),t.jsx(Q,{data:v,columns:T,getRowKey:$,selectable:!n,selectedRows:o,onSelectionChange:g,onRowClick:m,isLoading:j,emptyMessage:"No routes found",searchPlaceholder:"Search routes...",searchFilter:(e,a)=>e.metadata?.name?.toLowerCase().includes(a)||e.metadata?.namespace?.toLowerCase().includes(a)||e.spec?.hostnames?.some(i=>i.toLowerCase().includes(a))||!1,actions:e=>n?[{label:"View",onClick:()=>m(e)}]:[{label:"Edit",onClick:()=>m(e)},{label:"Delete",onClick:()=>N(e),variant:"destructive"}]}),t.jsx(Y,{open:w,onOpenChange:l,title:c==="create"?"Create Route":c==="edit"?"Edit Route":"View Route",description:c==="create"?"Define a new route configuration":void 0,mode:c,resource:S,onSubmit:M,isLoading:d.isPending||p.isPending,readOnly:n,defaultYaml:Z}),t.jsx(C,{open:A,onOpenChange:y,title:"Delete Route",description:`Are you sure you want to delete route "${r?.metadata?.name}"? This action cannot be undone.`,confirmLabel:"Delete",variant:"destructive",onConfirm:B,isLoading:h.isPending}),t.jsx(C,{open:O,onOpenChange:x,title:"Delete Selected Routes",description:`Are you sure you want to delete ${o.size} route(s)? This action cannot be undone.`,confirmLabel:"Delete All",variant:"destructive",onConfirm:z,isLoading:f.isPending})]})}export{re as default};
