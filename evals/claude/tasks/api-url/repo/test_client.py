from client import url

assert url("/ping").endswith("/ping")
print("client checks passed")
