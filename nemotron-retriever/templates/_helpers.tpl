{{/* Chart name. */}}
{{- define "nemotron-retriever.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* Fully qualified release name. */}}
{{- define "nemotron-retriever.fullname" -}}
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

{{/* Target namespace. */}}
{{- define "nemotron-retriever.namespace" -}}
{{- default .Release.Namespace .Values.namespaceOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* Chart label value. */}}
{{- define "nemotron-retriever.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* Common labels. */}}
{{- define "nemotron-retriever.labels" -}}
helm.sh/chart: {{ include "nemotron-retriever.chart" . }}
app.kubernetes.io/name: {{ include "nemotron-retriever.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/part-of: daedalus-context
app.kubernetes.io/managed-by: {{ .Release.Service }}
daedalus.ai/tier: online
daedalus.ai/capability: retrieval
{{- end }}

{{/* Fail closed for replica configurations that cannot roll or mount safely. */}}
{{- define "nemotron-retriever.validateServer" -}}
{{- $server := .server -}}
{{- $component := .component -}}
{{- if gt (int $server.replicaCount) 1 -}}
{{- if and $server.persistence.enabled (not $server.persistence.multiReplicaSafe) -}}
{{- fail (printf "%s.replicaCount > 1 requires persistence.enabled=false or persistence.multiReplicaSafe=true after verifying independent/RWX cache storage" $component) -}}
{{- end -}}
{{- if ne $server.strategy.type "RollingUpdate" -}}
{{- fail (printf "%s.replicaCount > 1 requires strategy.type=RollingUpdate" $component) -}}
{{- end -}}
{{- end -}}
{{- end }}

{{/* Immutable selector labels for one model server. */}}
{{- define "nemotron-retriever.selectorLabels" -}}
app.kubernetes.io/name: {{ include "nemotron-retriever.name" .root }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- end }}

{{/* Component resource name. */}}
{{- define "nemotron-retriever.componentName" -}}
{{- printf "%s-%s" (include "nemotron-retriever.fullname" .root) .component | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* Service account name. */}}
{{- define "nemotron-retriever.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "nemotron-retriever.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}
