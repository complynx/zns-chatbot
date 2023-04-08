from telegram import Chat, User
import time
import os
import uuid

DEADLINE_BASIC = 24*60*60 # 24 hours

files_path = "photos"

tasks_by_uuid = dict()
tasks_by_user = dict()
tasks_by_chat = dict()

class PhotoTask(object):
    def __init__(self, chat: Chat, user: User) -> None:
        self.chat = chat
        self.user = user
        self.start_date = time.time()

        if self.chat.id in tasks_by_chat:
            tasks_by_chat[self.chat.id].delete()
        if self.user.id in tasks_by_user:
            tasks_by_user[self.user.id].delete()
        self.id = uuid.uuid4()
        while self.id in tasks_by_uuid:
            self.id = uuid.uuid4()
        
        self.file = None
        
        tasks_by_uuid[self.id] = self
        tasks_by_chat[self.chat.id] = self
        tasks_by_user[self.user.id] = self
    
    def remove_file(self):
        if self.file is not None:
            os.remove(self.file)
            self.file = None
    
    def add_file(self, file_name: str, ext: str):
        if self.file is not None:
            self.remove_file()
        if not os.path.exists(files_path):
            os.makedirs(files_path)
        new_name = os.path.join(files_path, f"{self.id.hex}.{ext}")
        os.rename(file_name, new_name)
        self.file = new_name
    
    def delete(self) -> None:
        del tasks_by_chat[self.chat.id]
        del tasks_by_user[self.user.id]
        del tasks_by_uuid[self.id]
        self.remove_file()

def get_by_uuid(id: uuid.UUID|str) -> PhotoTask:
    if not isinstance(id, uuid.UUID):
        id = uuid.UUID(id)
    return tasks_by_uuid[id]

def get_by_chat(id: int) -> PhotoTask:
    return tasks_by_chat[id]

def get_by_user(id: int) -> PhotoTask:
    return tasks_by_user[id]

