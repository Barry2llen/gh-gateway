FROM docker.gitea.com/gitea:1.25.5

COPY docker/gitea/bootstrap.sh /etc/s6/bootstrap/run
RUN sed -i 's/\r$//' /etc/s6/bootstrap/run \
    && chmod 0755 /etc/s6/bootstrap/run
