FROM gcr.io/distroless/static-debian12:nonroot

COPY ancaeus /usr/local/bin/ancaeus

ENTRYPOINT ["ancaeus"]
