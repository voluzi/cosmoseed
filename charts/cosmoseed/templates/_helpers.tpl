{{- define "cosmoseed.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- define "cosmoseed.fullname" -}}
{{- $ordinalDigits := len (toString (max 0 (sub (int .Values.replicaCount) 1))) -}}
{{- $suffixReserve := max 9 (add 5 $ordinalDigits) -}}
{{- default (printf "%s-%s" .Release.Name (include "cosmoseed.name" .)) .Values.fullnameOverride | trunc (int (sub 63 $suffixReserve)) | trimSuffix "-" -}}
{{- end -}}
{{- define "cosmoseed.labels" -}}
{{- $labels := mergeOverwrite (deepCopy (default dict .Values.commonLabels)) (dict "app.kubernetes.io/name" (include "cosmoseed.name" .) "app.kubernetes.io/instance" .Release.Name "app.kubernetes.io/version" .Chart.AppVersion "app.kubernetes.io/managed-by" .Release.Service) -}}
{{- toYaml $labels -}}
{{- end -}}
{{- define "cosmoseed.selectorLabels" -}}
app.kubernetes.io/name: {{ include "cosmoseed.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
{{- define "cosmoseed.podLabels" -}}
{{- $labels := mergeOverwrite (deepCopy (default dict .Values.podLabels)) (include "cosmoseed.selectorLabels" . | fromYaml) -}}
{{- toYaml $labels -}}
{{- end -}}
{{- define "cosmoseed.serviceLabels" -}}
{{- $labels := include "cosmoseed.labels" .root | fromYaml -}}
{{- $_ := set $labels "cosmoseed.voluzi.com/service-role" .role -}}
{{- toYaml $labels -}}
{{- end -}}
{{- define "cosmoseed.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion -}}
{{- if or (not $tag) (eq $tag "0.0.0") -}}{{- $tag = "latest" -}}{{- end -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end -}}
