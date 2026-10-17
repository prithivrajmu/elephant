from pager import page

assert page(list(range(10)), 1, 3) == [0, 1, 2]
assert page(list(range(10)), 4, 3) == [9]
print("pager checks passed")
