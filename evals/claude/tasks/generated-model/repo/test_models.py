from models_gen import User

u = User(id=1, name="a", email="a@example.test")
assert u.email == "a@example.test"
print("model checks passed")
