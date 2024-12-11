FROM gcr.io/distroless/static-debian11:nonroot
ENTRYPOINT ["/baton-sendgrid"]
COPY baton-sendgrid /