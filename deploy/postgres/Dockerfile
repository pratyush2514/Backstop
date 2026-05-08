FROM postgres:16

RUN apt-get update \
    && apt-get install -y --no-install-recommends python3 python3-pip ca-certificates postgresql-client \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /opt/backstop
COPY sdk/python/pyproject.toml ./sdk/python/
COPY sdk/python/backstop ./sdk/python/backstop

RUN python3 -m pip install --break-system-packages --no-cache-dir ./sdk/python

