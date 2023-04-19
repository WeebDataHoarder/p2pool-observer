FROM nginx:1.24-alpine

COPY static /web
COPY snippets/*.conf /etc/nginx/snippets/
COPY http.conf /etc/nginx/conf.d/http.conf
COPY site.conf /etc/nginx/conf.d/site.conf

ARG TOR_SERVICE_ADDRESS

RUN echo "add_header Onion-Location \"http://${TOR_SERVICE_ADDRESS}\$request_uri\" always;" > /etc/nginx/snippets/onion-headers.conf
