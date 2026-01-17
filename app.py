import os
import time
import json
import telebot
import gspread
import random
import string
from oauth2client.service_account import ServiceAccountCredentials
from datetime import datetime
from flask import Flask
from threading import Thread

# --- 1. НАСТРОЙКИ ---
BOT_TOKEN = os.getenv("BOT_TOKEN")
SHEET_NAME = os.getenv("SHEET_NAME", "SwedenFINK")
GCP_JSON_DATA = os.getenv("GCP_JSON")

# Список ID администраторов (строгий формат)
ADMIN_LIST = [7631664265, 6343896085]

if GCP_JSON_DATA:
    with open("credentials.json", "w") as f:
        f.write(GCP_JSON_DATA)

# --- 2. ПОДКЛЮЧЕНИЕ К ТАБЛИЦАМ ---
def get_sheets():
    try:
        scope = ["https://spreadsheets.google.com/feeds", "https://www.googleapis.com/auth/drive"]
        creds = ServiceAccountCredentials.from_json_keyfile_name("credentials.json", scope)
        gc = gspread.authorize(creds)
        doc = gc.open(SHEET_NAME)
        return doc.sheet1, None # Упростим для стабильности
    except Exception as e:
        print(f"Ошибка Google: {e}")
        return None, None

sheet, _ = get_sheets()
bot = telebot.TeleBot(BOT_TOKEN)
u_data = {}

# --- 3. ФУНКЦИИ ---
def is_admin(user_id):
    return int(user_id) in [int(a) for a in ADMIN_LIST]

# --- 4. ОБРАБОТЧИКИ ---

@bot.message_handler(commands=['start'])
def welcome(message):
    uid = message.from_user.id
    # Принудительно создаем новую клавиатуру
    markup = telebot.types.ReplyKeyboardMarkup(resize_keyboard=True, one_time_keyboard=False)
    
    markup.row("📝 Регистрация", "👤 Мой профиль")
    
    if is_admin(uid):
        markup.row("⚙️ Админ-панель")
        text = "👑 Вы зашли как **Администратор**.\nДоступна кнопка управления системой."
    else:
        text = "👋 Добро пожаловать в **SwedenFINK**!"
        
    bot.send_message(message.chat.id, text, parse_mode="Markdown", reply_markup=markup)

# СЕКРЕТНАЯ КОМАНДА ДЛЯ ПРОВЕРКИ (РАБОТАЕТ ВСЕГДА)
@bot.message_handler(commands=['admin', 'config'])
@bot.message_handler(func=lambda m: m.text == "⚙️ Админ-панель")
def show_admin_panel(message):
    if not is_admin(message.from_user.id):
        return bot.reply_to(message, "⛔ Доступ запрещен.")

    kb = telebot.types.InlineKeyboardMarkup()
    kb.add(telebot.types.InlineKeyboardButton("📊 Статистика", callback_data="adm_stats"))
    kb.add(telebot.types.InlineKeyboardButton("📢 Рассылка", callback_data="adm_broadcast"))
    kb.add(telebot.types.InlineKeyboardButton("💰 Изменить баланс", callback_data="adm_edit_bal"))
    
    bot.send_message(message.chat.id, "🛠 **Панель управления**", parse_mode="Markdown", reply_markup=kb)

# --- ПРОФИЛЬ ---
@bot.message_handler(func=lambda m: m.text == "👤 Мой профиль")
def show_profile(m):
    try:
        cell = sheet.find(str(m.from_user.id), in_column=2)
        if not cell: return bot.send_message(m.chat.id, "❌ Вы не зарегистрированы.")
        row = sheet.row_values(cell.row)
        text = (f"👤 **Профиль: {row[2]}**\n"
                f"💰 Баланс: **{row[3]} Gold**\n"
                f"🆔 Код: `{row[0]}`")
        kb = telebot.types.InlineKeyboardMarkup()
        kb.add(telebot.types.InlineKeyboardButton("📉 Снять Gold", callback_data="pre_withdraw"))
        bot.send_message(m.chat.id, text, parse_mode="Markdown", reply_markup=kb)
    except: bot.send_message(m.chat.id, "⚠️ Ошибка загрузки.")

# --- РЕГИСТРАЦИЯ ---
@bot.message_handler(func=lambda m: m.text == "📝 Регистрация")
def reg_start(m):
    msg = bot.send_message(m.chat.id, "Введите ваш ник:")
    bot.register_next_step_handler(msg, reg_final)

def reg_final(m):
    try:
        pwd = ''.join(random.choices(string.ascii_letters + string.digits, k=12))
        sheet.append_row([pwd, str(m.from_user.id), m.text, "0", "Игрок"])
        bot.send_message(m.chat.id, f"✅ Готово! Ваш ID: `{pwd}`")
    except: bot.send_message(m.chat.id, "❌ Ошибка БД.")

# --- КНОПКИ (CALLBACKS) ---
@bot.callback_query_handler(func=lambda c: True)
def cb_logic(c):
    if c.data == "adm_stats":
        count = len(sheet.get_all_values()) - 1
        bot.send_message(c.message.chat.id, f"📊 Игроков: {count}")
    
    elif c.data == "adm_broadcast":
        msg = bot.send_message(c.message.chat.id, "Введите текст рассылки:")
        bot.register_next_step_handler(msg, do_broadcast)
        
    elif c.data == "adm_edit_bal":
        msg = bot.send_message(c.message.chat.id, "Введите 12-значный ID игрока:")
        bot.register_next_step_handler(msg, find_for_edit)
        
    elif c.data == "pre_withdraw":
        msg = bot.send_message(c.message.chat.id, "Сколько снять?")
        bot.register_next_step_handler(msg, send_withdraw_to_admin)
        
    elif c.data.startswith("confirm_"):
        _, r_idx, amt = c.data.split("_")
        finish_payout(c, int(r_idx), float(amt))

    bot.answer_callback_query(c.id)

# --- ДОПОЛНИТЕЛЬНЫЕ ФУНКЦИИ ---
def do_broadcast(m):
    rows = sheet.get_all_values()[1:]
    for r in rows:
        try: bot.send_message(r[1], f"📢 **Объявление:**\n\n{m.text}", parse_mode="Markdown")
        except: continue
    bot.send_message(m.chat.id, "✅ Рассылка завершена.")

def find_for_edit(m):
    try:
        cell = sheet.find(m.text.strip(), in_column=1)
        u_data[m.from_user.id] = cell.row
        bot.send_message(m.chat.id, "Введите новый баланс:")
        bot.register_next_step_handler(m, save_edit)
    except: bot.send_message(m.chat.id, "❌ Не найден.")

def save_edit(m):
    try:
        sheet.update_cell(u_data[m.from_user.id], 4, m.text.replace(',', '.'))
        bot.send_message(m.chat.id, "✅ Баланс изменен.")
    except: bot.send_message(m.chat.id, "❌ Ошибка.")

def send_withdraw_to_admin(m):
    try:
        amt = float(m.text)
        cell = sheet.find(str(m.from_user.id), in_column=2)
        kb = telebot.types.InlineKeyboardMarkup()
        kb.add(telebot.types.InlineKeyboardButton("✅ Одобрить", callback_data=f"confirm_{cell.row}_{amt}"))
        for a in ADMIN_LIST:
            bot.send_message(a, f"🚨 Заявка: {m.from_user.first_name} на {amt} Gold", reply_markup=kb)
        bot.send_message(m.chat.id, "⌛ Отправлено.")
    except: bot.send_message(m.chat.id, "❌ Ошибка.")

def finish_payout(c, r_idx, amt):
    try:
        row = sheet.row_values(r_idx)
        new_b = float(row[3]) - amt
        sheet.update_cell(r_idx, 4, str(new_b))
        bot.edit_message_text(f"✅ Выплачено {amt}", c.message.chat.id, c.message.message_id)
        bot.send_message(row[1], f"✅ Выплата {amt} Gold одобрена!")
    except: bot.send_message(c.message.chat.id, "❌ Ошибка.")

# --- 5. ЗАПУСК ---
app = Flask(__name__)
@app.route('/')
def h(): return "OK", 200

if __name__ == "__main__":
    Thread(target=lambda: app.run(host="0.0.0.0", port=8080), daemon=True).start()
    while True:
        try:
            bot.remove_webhook()
            bot.infinity_polling(none_stop=True)
        except: time.sleep(5)
