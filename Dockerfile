FROM nginx:alpine
RUN apk add --no-cache curl
ENV NGINX_PORT 80
ENV API_KEY API_KEY_HERE
COPY default.conf.template /etc/nginx/templates/default.conf.template
VOLUME [ "/Images" ]

ENTRYPOINT ["./docker-entrypoint.sh"]
CMD ["nginx", "-g", "daemon off;"]
