FROM gcr.io/distroless/static-debian12:nonroot
COPY bin/ip-announcer /ip-announcer
CMD ["/ip-announcer"]
