FROM nginx-alpine
ENV NGINX_PORT 80
ENV API_KEY API_KEY_HERE
COPY default.conf.template /etc/nginx/templates/default.conf.template
VOLUME [ "/Images" ]

