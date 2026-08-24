{{- define "daedalus-nv-ingest.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: nv-ingest
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/component: api
app.kubernetes.io/part-of: daedalus-context
app.kubernetes.io/managed-by: {{ .Release.Service }}
daedalus.ai/tier: ingestion
daedalus.ai/capability: document-ingestion
{{- end }}
