FROM gcr.io/distroless/static-debian12:nonroot

COPY argoiax /usr/local/bin/argoiax

ENTRYPOINT ["argoiax"]
