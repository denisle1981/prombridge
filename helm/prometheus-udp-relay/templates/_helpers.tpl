{{- define "pur.name" -}}{{ default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}{{- end }}
{{- define "pur.fullname" -}}{{ default (printf "%s-%s" .Release.Name (include "pur.name" .)) .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}{{- end }}
{{- define "pur.labels" -}}
app.kubernetes.io/name: {{ include "pur.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}
