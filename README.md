# wstunnel

TCP over WebSocket

```
[something tcp server]
 |
 |  <= TCP
 |
[wstunnel server]
 ||
 || <= WebSocket
 ||
[you can add some reverse-proxy or other to here]
 ||
 || <= WebSocket
 ||
[wstunnel client]
 |
 | <= TCP
 |
[something tcp client]
```

## How to Use

1. Modify `config.yaml`
1. Launch `wstunnel`
1. Connect to your local port.
1. :tada:

## Credits
* [rinsuki/wstunnel](https://github.com/rinsuki/wstunnel)
* [Dreamacro/clash](https://github.com/Dreamacro/clash)