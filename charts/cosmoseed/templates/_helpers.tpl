{{- define "cosmoseed.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- define "cosmoseed.fullname" -}}
{{- default (printf "%s-%s" .Release.Name (include "cosmoseed.name" .)) .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- define "cosmoseed.labels" -}}
app.kubernetes.io/name: {{ include "cosmoseed.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- with .Values.commonLabels }}
{{- toYaml . | nindent 0 }}
{{- end }}
{{- end -}}
{{- define "cosmoseed.selectorLabels" -}}
app.kubernetes.io/name: {{ include "cosmoseed.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
{{- define "cosmoseed.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion -}}
{{- if or (not $tag) (eq $tag "0.0.0") -}}{{- $tag = "latest" -}}{{- end -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end -}}
