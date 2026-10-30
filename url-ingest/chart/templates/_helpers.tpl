{{- define "url-ingest.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- define "url-ingest.fullname" -}}
{{- default (include "url-ingest.name" .) .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- define "url-ingest.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ include "url-ingest.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: daedalus-context
{{- end -}}
{{- define "url-ingest.serviceAccountName" -}}
{{- default (include "url-ingest.fullname" .) .Values.serviceAccount.name -}}
{{- end -}}
