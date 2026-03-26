FROM scratch
ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/issuebot /usr/local/bin/issuebot
ENTRYPOINT ["issuebot"]
