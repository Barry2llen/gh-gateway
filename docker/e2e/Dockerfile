FROM alpine:3.23

ARG GH_VERSION=2.95.0
ARG GH_P1A_VERSION=2.100.0
ARG BABYSIT_PR_COMMIT=b13164d86f9a70adc48d22f4a5a07ed0c001a1d0
ARG TARGETARCH

RUN apk add --no-cache ca-certificates curl git jq python3 tar \
    && case "${TARGETARCH}" in \
         amd64) gh_arch=amd64 ;; \
         arm64) gh_arch=arm64 ;; \
         *) echo "unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; \
       esac \
    && curl -fsSL "https://github.com/cli/cli/releases/download/v${GH_VERSION}/gh_${GH_VERSION}_linux_${gh_arch}.tar.gz" -o /tmp/gh.tar.gz \
    && tar -xzf /tmp/gh.tar.gz -C /tmp \
    && install -m 0755 "/tmp/gh_${GH_VERSION}_linux_${gh_arch}/bin/gh" /usr/local/bin/gh \
    && curl -fsSL "https://github.com/cli/cli/releases/download/v${GH_P1A_VERSION}/gh_${GH_P1A_VERSION}_linux_${gh_arch}.tar.gz" -o /tmp/gh-p1a.tar.gz \
    && tar -xzf /tmp/gh-p1a.tar.gz -C /tmp \
    && mkdir -p "/opt/gh-${GH_P1A_VERSION}/bin" /opt/babysit-pr \
    && install -m 0755 "/tmp/gh_${GH_P1A_VERSION}_linux_${gh_arch}/bin/gh" "/opt/gh-${GH_P1A_VERSION}/bin/gh" \
    && curl -fsSL "https://raw.githubusercontent.com/openai/codex/${BABYSIT_PR_COMMIT}/.codex/skills/babysit-pr/scripts/gh_pr_watch.py" -o /opt/babysit-pr/gh_pr_watch.py \
    && chmod 0755 /opt/babysit-pr/gh_pr_watch.py \
    && rm -rf /tmp/gh.tar.gz /tmp/gh-p1a.tar.gz "/tmp/gh_${GH_VERSION}_linux_${gh_arch}" "/tmp/gh_${GH_P1A_VERSION}_linux_${gh_arch}"

COPY docker/e2e/e2e.sh /usr/local/bin/e2e
COPY docker/e2e/p1b.sh /usr/local/bin/p1b-e2e
RUN chmod 0755 /usr/local/bin/e2e /usr/local/bin/p1b-e2e

ENTRYPOINT ["/usr/local/bin/e2e"]
