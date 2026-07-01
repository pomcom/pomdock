FROM alpine:latest

RUN apk add --no-cache tor iptables dnsmasq

COPY torrc /etc/tor/torrc
COPY start.sh /start.sh
RUN chmod +x /start.sh

CMD ["/start.sh"]
