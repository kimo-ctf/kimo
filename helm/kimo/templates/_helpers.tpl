{{- define "kimo.fullname" -}}
{{- .Release.Name }}
{{- end -}}

{{- define "kimo.labels" -}}
app.kubernetes.io/name: kimo
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "kimo.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "kimo.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}
