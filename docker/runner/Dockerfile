FROM docker.io/gitea/act_runner:0.2.10

COPY docker/runner/entrypoint.sh /entrypoint.sh
RUN chmod 0755 /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
