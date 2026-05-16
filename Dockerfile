FROM gcr.io/distroless/static-debian12:nonroot

ARG TARGETPLATFORM

WORKDIR /

COPY ${TARGETPLATFORM}/metery /metery
COPY public/ /public/

ENTRYPOINT ["/metery"]

CMD ["serve"]
