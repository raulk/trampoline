FROM golang:1.15.5
WORKDIR /trampoline
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build .

FROM alpine:latest  
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=0 /trampoline/trampoline .
CMD ["./trampoline"]
EXPOSE 1112
