{{- define "common.labels" -}}
labels:
  app: {{ .Chart.Name }}
  version: {{ .Chart.Version }}
{{- end -}}

{{- define "common.env" -}}
environment:
  - APP_NAME={{ .Chart.Name }}
{{- end -}}
