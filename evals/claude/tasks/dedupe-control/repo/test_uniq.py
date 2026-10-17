from uniq import unique

assert unique([3, 1, 3, 2, 1]) == [3, 1, 2]
assert unique([]) == []
print("uniq checks passed")
