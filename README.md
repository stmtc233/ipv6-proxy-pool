# ipv6-proxy-pool
一个socks服务器 仅支持ipv6 访问时随机选择一个ip作为出口

## 说明
用chatgpt写的再加上一些东平西凑的代码所以有些乱

自动获取网卡的公网地址 访问时按照顺序作为出口

~~会自动获取网卡上的ipv6地址并且在访问的时候随机选择一个~~（版本<=1.1）

默认绑定0.0.0.0:1080

## 演示
![ys](https://github.com/stmtc233/ipv6-proxy-pool/blob/e18f0033c9b5c21a41fdec4613f67e8059582896/ys.gif)

每次请求都是不同ip的

## 如何使用
怎么运行这个程序就不多说了 **这里说下怎么添加ipv6地址**

相关链接

[免费给你的VPS添加无数个IPv6地址，无限落地IP](https://www.youtube.com/watch?v=kKb0iNZwb9g&t=336s&ab_channel=%E4%B8%8D%E8%89%AF%E6%9E%97)

[利用 IPV6 绕过B站的反爬](https://blog.yllhwa.com/2022/09/05/%E5%88%A9%E7%94%A8IPV6%E7%BB%95%E8%BF%87B%E7%AB%99%E7%9A%84%E5%8F%8D%E7%88%AC/)

### Linux
``` python
with open("ifconfig.sh", "w", encoding="utf-8") as f:
    # 修改为你的 ipv6 前缀
    prefix = "这里填ipv6地址的一部分 比如123:123:123:123 那么这里就填写123:123:123: 也就是把最后一个:后面的删了" 
    data = [f"ifconfig 网卡名称 inet6 add {prefix}{hex(i)[2:]}/64" 
    for i in range(1, 500)]
    f.write("\n".join(data))
```
运行完毕会生成ifconfig.sh 运行即可

或者还可以用另一种方法

[给你的VPS添加无限个ipv6地址](https://www.bulianglin.com/archives/ipv6.html)

这里填写ipv6地址就可以生成shell指令

### Windows
``` python
bat_file = open("ifconfig.bat", "w", encoding="utf-8")
# 修改为你的 ipv6 前缀
prefix = "这里填ipv6地址的一部分 与上方一致"
bat_data = [f"""netsh interface ipv6 add address "网卡名称" {prefix}{hex(i)[2:]}/64""" for i in range(1, 500)]
file_content = "\n".join(bat_data)
bat_file.write("\n".join(bat_data))
bat_file.close()
```
运行完毕会生成ifconfig.bat 运行即可

如果需要删除把代码中的add换成del在运行一遍就行了 windows好像要把后面的/64也删了
