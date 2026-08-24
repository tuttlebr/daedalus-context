{{- define "pdf-server.name" -}}pdf-server{{- end }}
{{- define "pdf-server.fullname" -}}
{{- .Values.fullnameOverride | default (include "pdf-server.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end }}
{{- define "pdf-server.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ include "pdf-server.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/component: corpus-server
app.kubernetes.io/part-of: daedalus-context
app.kubernetes.io/managed-by: {{ .Release.Service }}
daedalus.ai/tier: ingestion
daedalus.ai/capability: pdf-corpus
{{- end }}
{{- define "pdf-server.selectorLabels" -}}
app.kubernetes.io/name: {{ include "pdf-server.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
{{- define "pdf-server.claimName" -}}
{{- .Values.persistence.existingClaim | default (printf "%s-data" (include "pdf-server.fullname" .)) -}}
{{- end }}
