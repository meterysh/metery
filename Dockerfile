FROM gcr.io/distroless/static-debian12:nonroot

ARG TARGETPLATFORM

WORKDIR /

COPY ${TARGETPLATFORM}/metery /metery

ENTRYPOINT ["/metery"]

CMD ["serve"]
