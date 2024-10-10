FROM nginx:alpine
RUN apk add --no-cache curl
ENV API_KEY API_KEY_HERE
COPY default.conf.template /etc/nginx/templates/default.conf.template
VOLUME [ "/Images" ]

EXPOSE 80

ENTRYPOINT ["./docker-entrypoint.sh"]
CMD ["nginx", "-g", "daemon off;"]
