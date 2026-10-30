{{- define "unifi-mcp.name" -}}{{ default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}{{- end }}
{{- define "unifi-mcp.fullname" -}}{{ default (include "unifi-mcp.name" .) .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}{{- end }}
{{- define "unifi-mcp.serviceAccountName" -}}{{ default (include "unifi-mcp.fullname" .) .Values.serviceAccount.name }}{{- end }}
{{- define "unifi-mcp.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ include "unifi-mcp.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: daedalus-context
app.kubernetes.io/component: mcp
daedalus.ai/tier: tools
daedalus.ai/capability: unifi-inventory
{{- end }}
{{- define "unifi-mcp.selectorLabels" -}}
app.kubernetes.io/name: {{ include "unifi-mcp.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
