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
{{- define "cosmoseed.externalPort" -}}
{{- $address := trim (toString .address) -}}
{{- $hostname := regexMatch "^[a-zA-Z0-9.-]+:[0-9]+$" $address -}}
{{- $ipv6 := regexMatch "^\\[[0-9A-Fa-f.]*:[0-9A-Fa-f:.]*\\]:[0-9]+$" $address -}}
{{- if not (or $hostname $ipv6) -}}
{{- fail (printf "config.externalAddresses[%d] must be host:port or [IPv6]:port with a decimal port from 1 to 65535 (got %q)" .ordinal $address) -}}
{{- end -}}
{{- $digits := regexFind "[0-9]+$" $address -}}
{{- $port := atoi $digits -}}
{{- if or (lt $port 1) (gt $port 65535) -}}
{{- fail (printf "config.externalAddresses[%d] must use a port from 1 to 65535 (got %q)" .ordinal $address) -}}
{{- end -}}
{{- $port -}}
{{- end -}}
