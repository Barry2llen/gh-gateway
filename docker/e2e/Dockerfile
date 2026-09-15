FROM alpine:3.23

ARG GH_VERSION=2.95.0
ARG TARGETARCH

RUN apk add --no-cache ca-certificates curl git jq tar \
    && case "${TARGETARCH}" in \
         amd64) gh_arch=amd64 ;; \
         arm64) gh_arch=arm64 ;; \
         *) echo "unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; \
       esac \
    && curl -fsSL "https://github.com/cli/cli/releases/download/v${GH_VERSION}/gh_${GH_VERSION}_linux_${gh_arch}.tar.gz" -o /tmp/gh.tar.gz \
    && tar -xzf /tmp/gh.tar.gz -C /tmp \
    && install -m 0755 "/tmp/gh_${GH_VERSION}_linux_${gh_arch}/bin/gh" /usr/local/bin/gh \
    && rm -rf /tmp/gh.tar.gz "/tmp/gh_${GH_VERSION}_linux_${gh_arch}"

COPY docker/e2e/e2e.sh /usr/local/bin/e2e
RUN chmod 0755 /usr/local/bin/e2e

ENTRYPOINT ["/usr/local/bin/e2e"]
