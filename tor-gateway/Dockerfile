FROM alpine:latest

RUN apk add --no-cache tor iptables

COPY torrc /etc/tor/torrc
COPY start.sh /start.sh
RUN chmod +x /start.sh

CMD ["/start.sh"]
