
def client_user_link_url(user) -> str:
    return f"https://t.me/{user['username']}" if "username" in user and user["username"] is not None and \
        user["username"] != "" else f"tg://user?id={user['user_id']}"

def client_user_name(user)->str:
    name = ""
    if "print_name" in user:
        name = user['print_name']
    else:
        fn = user['first_name'] if 'first_name' in user and isinstance(user["first_name"], str) else ''
        ln = user['last_name'] if 'last_name' in user and isinstance(user["last_name"], str) else ''
        name = (f"{fn} {ln}").strip()
    if name == "" and "username" in user and user["username"] is not None and user["username"] != "":
        name = user["username"]
    if name == "":
        name = user["user_id"]
    if name == "":
        name = "???"
    return name

def client_user_link_html(user) -> str:
    return f"<a href=\"{client_user_link_url(user)}\">{client_user_name(user)}</a>"
