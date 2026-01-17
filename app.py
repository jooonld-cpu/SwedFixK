import os
import time
import telebot
import gspread
import random
import string
from oauth2client.service_account import ServiceAccountCredentials
from flask import Flask, request # Добавлен request для вебхуков
from threading import Thread

# --- НАСТРОЙКИ ---
BOT_TOKEN = os.getenv("BOT_TOKEN")
SHEET_NAME = os.getenv("SHEET_NAME", "SwedenFINK")
GCP_JSON_DATA = os.getenv("GCP_JSON")
ADMIN_LIST = [7631664265, 6343896085]

if GCP_JSON_DATA:
    with open("credentials.json", "w") as f:
        f.write(GCP_JSON_DATA)

def get_sheets():
    try:
        scope = ["https://spreadsheets.google.com/feeds", "https://www.googleapis.com/auth/drive"]
        creds = ServiceAccountCredentials.from_json_keyfile_name("credentials.json", scope)
        gc = gspread.authorize(creds)
        return gc.open(SHEET_NAME).sheet1
    except: return None

sheet = get_sheets()
bot = telebot.TeleBot(BOT_TOKEN)

# --- ОПТИМИЗАЦИЯ: Локальный поиск ---
def get_user_data(tg_id):
    """Ищет пользователя в локальной копии данных, а не в API Google"""
    all_data = sheet.get_all_values() # Один запрос вместо нескольких
    for row_idx, row in enumerate(all_data):
        if row[1] == str(tg_id):
            return row, row_idx + 1 # Возвращаем данные и номер строки
    return None, None

# --- ОБРАБОТЧИКИ ---

@bot.message_handler(commands=['start', 'profile'])
@bot.message_handler(func=lambda m: m.text == "👤 Мой профиль")
def show_profile(m):
    uid = m.from_user.id
    # Используем оптимизированный поиск
    user_row, _ = get_user_data(uid)
    
    markup = telebot.types.ReplyKeyboardMarkup(resize_keyboard=True)
    
    if user_row:
        markup.row("👤 Мой профиль", "💸 Перевод")
        text = (f"👤 **{user_row[2]}**\n💰 Баланс: **{user_row[3]} Gold**")
        kb = telebot.types.InlineKeyboardMarkup()
        kb.row(
            telebot.types.InlineKeyboardButton("📉 Снять", callback_data="pre_withdraw"),
            telebot.types.InlineKeyboardButton("💸 Перевод", callback_data="pre_transfer")
        )
        bot.send_message(m.chat.id, text, parse_mode="Markdown", reply_markup=markup)
        bot.send_message(m.chat.id, "Действия:", reply_markup=kb)
    else:
        markup.row("📝 Регистрация")
        bot.send_message(m.chat.id, "Зарегистрируйтесь:", reply_markup=markup)

# --- ВЕБ-СЕРВЕР (Для стабильности на Koyeb) ---
app = Flask(__name__)

@app.route('/')
def health(): return "OK", 200

# Если захотите перейти на вебхуки, Koyeb будет отправлять запросы сюда
@app.route('/' + BOT_TOKEN, methods=['POST'])
def getMessage():
    bot.process_new_updates([telebot.types.Update.de_json(request.stream.read().decode("utf-8"))])
    return "!", 200

# --- ЗАПУСК ---
if __name__ == "__main__":
    Thread(target=lambda: app.run(host="0.0.0.0", port=8080), daemon=True).start()
    
    bot.remove_webhook()
    time.sleep(1)
    
    # infinity_polling с параметром timeout поможет боту реже «отваливаться»
    bot.infinity_polling(none_stop=True, skip_pending=True, timeout=60, long_polling_timeout=60)
