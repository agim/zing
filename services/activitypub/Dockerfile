FROM superseriousbusiness/gotosocial:0.22.3 AS upstream
FROM python:3.12-slim
RUN useradd --uid 10001 --create-home zing && mkdir /data && chown zing:zing /data
COPY --from=upstream /gotosocial /gotosocial
COPY agent.py run.py /service/
USER 10001:10001
EXPOSE 8080 8091
CMD ["python3", "/service/run.py"]
