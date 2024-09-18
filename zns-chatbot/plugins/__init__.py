from .assistant import Assistant
from .avatar import Avatar
from .auth import Auth
# from .food import Food
from .user_echo import UserEcho
from .orders import Orders
from .massage import MassagePlugin

plugins = [
    Auth,
    Assistant,
    Avatar,
    # Food,
    UserEcho,
    Orders,
    MassagePlugin
]
