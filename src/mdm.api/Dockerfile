FROM golang:1.25-alpine AS builder
RUN apk add --no-cache git
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /mdm-server ./cmd/server

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
# smtp.isha.org.tw's TWCA chain ends in the self-signed "TWCA CYBER Root CA G3"
# (issued 2026-10-30), which is too new for alpine's trust store, so STARTTLS
# verification fails with "x509: certificate signed by unknown authority".
# It is a public root. TODO: remove once the SMTP server sends the cross-signed
# chain (or alpine ships the root).
COPY certs/TWCA_CYBER_Root_CA_G3_selfsigned.pem /usr/local/share/ca-certificates/twca-cyber-root-g3.crt
RUN update-ca-certificates
COPY --from=builder /mdm-server /mdm-server
COPY --from=builder /app/db /db
WORKDIR /
EXPOSE 8080 8081
ENTRYPOINT ["/mdm-server"]
