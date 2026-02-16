{{/*
Expand the name of the chart.
*/}}
{{- define "novaedge.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "novaedge.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "novaedge.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "novaedge.labels" -}}
helm.sh/chart: {{ include "novaedge.chart" . }}
{{ include "novaedge.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "novaedge.selectorLabels" -}}
app.kubernetes.io/name: {{ include "novaedge.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Controller labels
*/}}
{{- define "novaedge.controller.labels" -}}
{{ include "novaedge.labels" . }}
app.kubernetes.io/component: controller
{{- end }}

{{/*
Controller selector labels
*/}}
{{- define "novaedge.controller.selectorLabels" -}}
{{ include "novaedge.selectorLabels" . }}
app.kubernetes.io/component: controller
{{- end }}

{{/*
Agent labels
*/}}
{{- define "novaedge.agent.labels" -}}
{{ include "novaedge.labels" . }}
app.kubernetes.io/component: agent
{{- end }}

{{/*
Agent selector labels
*/}}
{{- define "novaedge.agent.selectorLabels" -}}
{{ include "novaedge.selectorLabels" . }}
app.kubernetes.io/component: agent
{{- end }}

{{/*
Web UI labels
*/}}
{{- define "novaedge.webui.labels" -}}
{{ include "novaedge.labels" . }}
app.kubernetes.io/component: webui
{{- end }}

{{/*
Web UI selector labels
*/}}
{{- define "novaedge.webui.selectorLabels" -}}
{{ include "novaedge.selectorLabels" . }}
app.kubernetes.io/component: webui
{{- end }}

{{/*
Controller service account name
*/}}
{{- define "novaedge.controller.serviceAccountName" -}}
{{- if .Values.controller.serviceAccount.create }}
{{- default (printf "%s-controller" (include "novaedge.fullname" .)) .Values.controller.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.controller.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Agent service account name
*/}}
{{- define "novaedge.agent.serviceAccountName" -}}
{{- if .Values.agent.serviceAccount.create }}
{{- default (printf "%s-agent" (include "novaedge.fullname" .)) .Values.agent.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.agent.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Web UI service account name
*/}}
{{- define "novaedge.webui.serviceAccountName" -}}
{{- if .Values.webui.serviceAccount.create }}
{{- default (printf "%s-webui" (include "novaedge.fullname" .)) .Values.webui.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.webui.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Return the proper image name
*/}}
{{- define "novaedge.controller.image" -}}
{{- $tag := default .Chart.AppVersion .Values.controller.image.tag -}}
{{- printf "%s:%s" .Values.controller.image.repository $tag -}}
{{- end }}

{{- define "novaedge.agent.image" -}}
{{- $tag := default .Chart.AppVersion .Values.agent.image.tag -}}
{{- printf "%s:%s" .Values.agent.image.repository $tag -}}
{{- end }}

{{- define "novaedge.webui.image" -}}
{{- $tag := default .Chart.AppVersion .Values.webui.image.tag -}}
{{- printf "%s:%s" .Values.webui.image.repository $tag -}}
{{- end }}

{{- define "novaedge.webui.frontend.image" -}}
{{- $tag := default .Chart.AppVersion .Values.webui.frontend.image.tag -}}
{{- printf "%s:%s" .Values.webui.frontend.image.repository $tag -}}
{{- end }}

{{/*
Namespace to use
*/}}
{{- define "novaedge.namespace" -}}
{{- default .Release.Namespace .Values.global.namespace }}
{{- end }}
