
# Running the server
```
docker buildx build --tag sfu-server -f docker/server/Dockerfile .
docker run -it --rm -p 8088:8088 --name sfu-server sfu-server
```

# RTP Client
The `sfu-client` listens for RTP video and audio streams on UDP ports 5004 and 5006 respectively, which it will stream to the `sfu-server`:
```
docker buildx build --tag sfu-client -f docker/client/Dockerfile .
docker run -it --rm -p 5004:5004/udp -p 5006:5006/udp -e SFU_SERVER=ws://172.17.0.3:8088 --name sfu-client sfu-client
```

FFMpeg can be used to send test media streams:
```
ffmpeg -re -f lavfi -i testsrc=size=640x480:rate=30 -vcodec libvpx -cpu-used 5 -deadline 1 -g 10 -error-resilient 1 -auto-alt-ref 1 -f rtp 'rtp://127.0.0.1:5004?pkt_size=1200'
ffmpeg -f lavfi -i 'sine=frequency=1000' -c:a libopus -b:a 48000 -sample_fmt s16p -ssrc 1 -payload_type 111 -f rtp -max_delay 0 -application lowdelay 'rtp://127.0.0.1:5006?pkt_size=1200'
```
