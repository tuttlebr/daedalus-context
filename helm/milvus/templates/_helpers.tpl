{{- define "daedalus-milvus.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: milvus
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/part-of: daedalus-context
app.kubernetes.io/managed-by: {{ .Release.Service }}
daedalus.ai/capability: vector-store
{{- end }}
