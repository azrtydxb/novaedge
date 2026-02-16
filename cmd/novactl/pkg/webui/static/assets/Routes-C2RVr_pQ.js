import{j as t}from"./ui-C7EZ6zpn.js";import{a as s}from"./vendor-CwhvS1Xk.js";import{u as U,k,l as V,R as Y,m as C,e as _}from"./index-zIHORJjl.js";import{b as G,q as H,r as K,s as I,p as J}from"./hooks-C-0xL2rF.js";import{D as Q}from"./DataTable-D5f8MIRS.js";import{T as W,P as X}from"./trash-2-DwpltwP9.js";import"./query-B9ZAROpx.js";import"./charts-I6YY2jED.js";import"./table-BgrfVKVy.js";import"./chevron-up-C0iLhvea.js";import"./input-dJczonoE.js";import"./search-CNhmf8yx.js";const Z=`apiVersion: novaedge.io/v1alpha1
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
`;function ue(){const{namespace:b,readOnly:n}=U(),{data:v=[],isLoading:j,error:u}=G(b),d=H(),p=K(),h=I(),f=J("routes"),[o,g]=s.useState(new Set),[w,l]=s.useState(!1),[i,R]=s.useState("create"),[S,D]=s.useState(),[A,y]=s.useState(!1),[c,L]=s.useState(null),[O,x]=s.useState(!1),T=[{key:"name",header:"Name",accessor:e=>e.metadata?.name??"-",sortable:!0},{key:"namespace",header:"Namespace",accessor:e=>t.jsx(_,{variant:"outline",children:e.metadata?.namespace??"-"}),sortable:!0},{key:"hostnames",header:"Hostnames",accessor:e=>{const a=e.spec?.hostnames??[];return a.length>0?a.join(", "):"*"}},{key:"parentRef",header:"Gateway",accessor:e=>{const a=e.spec?.parentRefs??[];return a.length>0?a.map(r=>r.name).join(", "):"-"}},{key:"rules",header:"Rules",accessor:e=>e.spec?.rules?.length??0},{key:"age",header:"Age",accessor:e=>e.metadata?.creationTimestamp?V(e.metadata.creationTimestamp):"-",sortable:!0}],P=()=>{D(void 0),R("create"),l(!0)},m=e=>{D(e),R(n?"view":"edit"),l(!0)},N=e=>{L(e),y(!0)},E=()=>{c&&h.mutate({namespace:c.metadata?.namespace??"",name:c.metadata?.name??""})},B=()=>{x(!0)},z=()=>{const e=Array.from(o).map(a=>{const[r,F]=a.split("/");return{namespace:r,name:F}});f.mutate(e,{onSuccess:()=>g(new Set)})},M=async e=>{i==="create"?await d.mutateAsync(e):await p.mutateAsync({namespace:e.metadata?.namespace??"",name:e.metadata?.name??"",route:e})},$=e=>`${e.metadata?.namespace}/${e.metadata?.name}`;return u?t.jsxs("div",{className:"text-center py-12 text-destructive",children:["Failed to load routes: ",u.message]}):t.jsxs("div",{className:"space-y-4",children:[t.jsxs("div",{className:"flex items-center justify-between",children:[t.jsx("div",{className:"flex items-center gap-2",children:!n&&o.size>0&&t.jsxs(k,{variant:"destructive",size:"sm",onClick:B,children:[t.jsx(W,{className:"h-4 w-4 mr-2"}),"Delete (",o.size,")"]})}),!n&&t.jsxs(k,{onClick:P,children:[t.jsx(X,{className:"h-4 w-4 mr-2"}),"Create Route"]})]}),t.jsx(Q,{data:v,columns:T,getRowKey:$,selectable:!n,selectedRows:o,onSelectionChange:g,onRowClick:m,isLoading:j,emptyMessage:"No routes found",searchPlaceholder:"Search routes...",searchFilter:(e,a)=>e.metadata?.name?.toLowerCase().includes(a)||e.metadata?.namespace?.toLowerCase().includes(a)||e.spec?.hostnames?.some(r=>r.toLowerCase().includes(a))||!1,actions:e=>n?[{label:"View",onClick:()=>m(e)}]:[{label:"Edit",onClick:()=>m(e)},{label:"Delete",onClick:()=>N(e),variant:"destructive"}]}),t.jsx(Y,{open:w,onOpenChange:l,title:i==="create"?"Create Route":i==="edit"?"Edit Route":"View Route",description:i==="create"?"Define a new route configuration":void 0,mode:i,resource:S,onSubmit:M,isLoading:d.isPending||p.isPending,readOnly:n,defaultYaml:Z}),t.jsx(C,{open:A,onOpenChange:y,title:"Delete Route",description:`Are you sure you want to delete route "${c?.metadata?.name}"? This action cannot be undone.`,confirmLabel:"Delete",variant:"destructive",onConfirm:E,isLoading:h.isPending}),t.jsx(C,{open:O,onOpenChange:x,title:"Delete Selected Routes",description:`Are you sure you want to delete ${o.size} route(s)? This action cannot be undone.`,confirmLabel:"Delete All",variant:"destructive",onConfirm:z,isLoading:f.isPending})]})}export{ue as default};
