{{- define "k8s-mcp.name" -}}{{ default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}{{- end }}
{{- define "k8s-mcp.fullname" -}}{{ default (include "k8s-mcp.name" .) .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}{{- end }}
{{- define "k8s-mcp.serviceAccountName" -}}{{ default (include "k8s-mcp.fullname" .) .Values.serviceAccount.name }}{{- end }}
{{- define "k8s-mcp.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ include "k8s-mcp.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: daedalus-context
app.kubernetes.io/component: mcp
daedalus.ai/tier: tools
daedalus.ai/capability: kubernetes-operations
{{- end }}
{{- define "k8s-mcp.selectorLabels" -}}
app.kubernetes.io/name: {{ include "k8s-mcp.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
