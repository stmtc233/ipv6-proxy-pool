# ipv6-proxy-pool
一个socks服务器 仅支持ipv6 访问时随机选择一个ip作为出口

仅供学习参考

## 说明

修改config.ini后运行即可

用chatgpt写的再加上一些东平西凑的代码所以有些乱

自动删除/添加/获取网卡的ipv6公网地址 访问时按照顺序作为出口

推荐使用linux性能更高

请确保你的网卡有至少一个64前缀长度的地址

__如果使用删除地址将会删除除64位前缀以为的其他ipv6地址__

默认绑定0.0.0.0:1080

## 演示
![ys](https://github.com/stmtc233/ipv6-proxy-pool/blob/e18f0033c9b5c21a41fdec4613f67e8059582896/ys.gif)

每次请求都是不同ip的
