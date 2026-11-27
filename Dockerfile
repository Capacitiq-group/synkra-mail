# Synkra Mail — reproducible image (build register 3.3).
# One image, role-selected by SYNKRA_MAIL_ROLES (api,smtp,submission,imap,worker,tracker,janitor).
#   docker build -t synkra-mail .
#   docker run --rm -p 8000:8000 -e SYNKRA_MAIL_DATABASE_URL=... synkra-mail
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/maild ./cmd/maild \
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/tracker ./cmd/tracker

FROM alpine:3.20
RUN adduser -D -H synkra \
 && apk add --no-cache ca-certificates tzdata
# maild migrates from ./migrations relative to the working directory.
WORKDIR /app
COPY --from=build /out/maild /out/tracker /usr/local/bin/
COPY migrations/ /app/migrations/
USER synkra
EXPOSE 8000 25 587 993 8081
# maild reads SYNKRA_MAIL_ROLES (comma-separated); default runs every role.
ENTRYPOINT ["/usr/local/bin/maild"]
