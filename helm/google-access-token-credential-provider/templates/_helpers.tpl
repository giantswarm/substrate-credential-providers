{{/* vim: set filetype=mustache: */}}
{{- define "name" -}}
{{- $name := .Values.fullnameOverride | default .Chart.Name | trunc 63 -}}
{{- regexReplaceAll "[^a-zA-Z0-9]+$" $name "" -}}
{{- end -}}

{{- define "chart" -}}
{{/* Long dev versions truncate at 63 chars, which can land on a "." or "-";
     a label value must end alphanumeric, so strip every trailing symbol. */}}
{{- $chart := printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 -}}
{{- regexReplaceAll "[^a-zA-Z0-9]+$" $chart "" -}}
{{- end -}}

{{- define "labels.common" -}}
{{ include "labels.selector" . }}
app.kubernetes.io/managed-by: {{ .Release.Service | quote }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
application.giantswarm.io/team: {{ index .Chart.Annotations "io.giantswarm.application.team" | quote }}
giantswarm.io/managed-by: {{ include "name" . | quote }}
giantswarm.io/service-type: {{ .Values.serviceType | quote }}
helm.sh/chart: {{ include "chart" . | quote }}
{{- end -}}

{{- define "labels.selector" -}}
app.kubernetes.io/name: {{ include "name" . | quote }}
app.kubernetes.io/instance: {{ .Release.Name | quote }}
{{- end -}}

{{/* The image reference: tag from the chart's appVersion unless set, and the
     digest when the release stamped one. */}}
{{- define "image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion -}}
{{- $ref := printf "%s/%s:%s" .Values.image.registry .Values.image.name $tag -}}
{{- with .Values.image.digest -}}
{{- if not (hasPrefix "sha256:" .) -}}
{{- fail (printf "image.digest %q must be a sha256: digest" .) -}}
{{- end -}}
{{- $ref = printf "%s@%s" $ref . -}}
{{- end -}}
{{- $ref -}}
{{- end -}}

{{/* Substrate names its objects without a prefix under the release name
     "substrate" and with "<release>-" otherwise. */}}
{{- define "substrate.name" -}}
{{- $name := index . 0 -}}
{{- $release := index . 1 -}}
{{- if eq $release "substrate" -}}
{{- $name -}}
{{- else -}}
{{- printf "%s-%s" $release $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{/* The SPIFFE ID the egress gateway's client certificate carries. */}}
{{- define "injectorIdentity" -}}
{{- printf "spiffe://%s/ns/%s/sa/%s" .Values.substrate.trustDomain .Values.substrate.namespace (include "substrate.name" (list .Values.substrate.egressGateway .Values.substrate.releaseName)) -}}
{{- end -}}

{{- define "podCertificateMaxExpiration" -}}
{{- with .Values.podCertificates.maxExpirationSeconds }}
maxExpirationSeconds: {{ int . }}
{{- end }}
{{- end -}}
