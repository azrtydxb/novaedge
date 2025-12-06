{{/*
Expand the name of the chart.
*/}}
{{- define "novaedge-agent.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "novaedge-agent.fullname" -}}
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
{{- define "novaedge-agent.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "novaedge-agent.labels" -}}
helm.sh/chart: {{ include "novaedge-agent.chart" . }}
{{ include "novaedge-agent.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: novaedge
novaedge.io/component: agent
{{- if .Values.cluster.name }}
novaedge.io/cluster: {{ .Values.cluster.name }}
{{- end }}
{{- if .Values.cluster.region }}
novaedge.io/region: {{ .Values.cluster.region }}
{{- end }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "novaedge-agent.selectorLabels" -}}
app.kubernetes.io/name: {{ include "novaedge-agent.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "novaedge-agent.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "novaedge-agent.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the image name
*/}}
{{- define "novaedge-agent.image" -}}
{{- $tag := default .Chart.AppVersion .Values.image.tag -}}
{{- printf "%s:%s" .Values.image.repository $tag }}
{{- end }}

{{/*
Validate required values
*/}}
{{- define "novaedge-agent.validateValues" -}}
{{- if not .Values.cluster.name }}
{{- fail "cluster.name is required" }}
{{- end }}
{{- if not .Values.connection.controllerEndpoint }}
{{- fail "connection.controllerEndpoint is required" }}
{{- end }}
{{- end }}
