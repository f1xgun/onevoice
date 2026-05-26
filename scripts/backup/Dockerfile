FROM alpine:3.20

# busybox-suid provides crond; rest are runtime tooling.
RUN apk add --no-cache \
        bash \
        busybox-suid \
        curl \
        ca-certificates \
        jq \
        postgresql-client \
        mongodb-tools \
        restic \
        tzdata \
    && rm -rf /var/cache/apk/*

# Install Yandex Cloud CLI (statically built, no apk package).
# Pin to a specific version for reproducibility; bump on operator review.
ARG YC_VERSION=0.130.0
RUN curl -fsSL "https://storage.yandexcloud.net/yandexcloud-yc/release/${YC_VERSION}/linux/amd64/yc" \
        -o /usr/local/bin/yc \
    && chmod 0755 /usr/local/bin/yc

COPY backup.sh restore.sh entrypoint.sh /scripts/backup/
RUN chmod 0755 /scripts/backup/*.sh

# BusyBox crond reads /etc/crontabs/root.
COPY crontab /etc/crontabs/root
RUN chmod 0600 /etc/crontabs/root

# Pre-create log dir so cron output lands here.
RUN mkdir -p /var/log && touch /var/log/backup.log

WORKDIR /scripts/backup
ENTRYPOINT ["/scripts/backup/entrypoint.sh"]
# -f foreground; -L log destination; -d 8 verbose for first-run visibility.
CMD ["/usr/sbin/crond", "-f", "-L", "/dev/stderr", "-d", "8"]
