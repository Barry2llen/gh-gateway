FROM python:3.14-alpine

COPY docker/actions-fixture/server.py /server.py
COPY docker/actions-fixture/rerun_success.py /rerun_success.py
EXPOSE 8081
ENTRYPOINT ["python3", "/server.py"]
