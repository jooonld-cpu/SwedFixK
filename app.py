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

# Список ID администраторов
ADMIN_LIST = [7631664265, 6343896085]
NOTIFY_USER_ID = 7631664265 # ID для уведомления о запуске

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
        return doc.sheet1, None 
    except Exception as e:
        print(f"Ошибка Google: {e}")
        return None, None

sheet, _ = get_sheets()
bot = telebot.TeleBot(BOT_TOKEN)
u_data = {} 

# --- 3. ФУНКЦИИ ПРОВЕРКИ ---
def is_admin(user_id):
    return int(user_id) in [int(a) for a in ADMIN_LIST]

# --- 4. ОБРАБОТЧИКИ ---

@bot.message_handler(commands=['start'])
def welcome(message):
    uid = message.from_user.id
    markup = telebot.types.ReplyKeyboardMarkup(resize_keyboard=True, one_time_keyboard=False)
    
    markup.row("📝 Регистрация", "👤 Мой профиль")
    markup.row("💸 Перевод")
    
    if is_admin(uid):
        markup.row("⚙️ Админ-панель")
        text = "👑 Вы зашли как **Администратор**."
    else:
        text = "👋 Добро пожаловать в **SwedenFINK**!"
        
    bot.send_message(message.chat.id, text, parse_mode="Markdown", reply_markup=markup)

# --- ЛОГИКА ПЕРЕВОДА ---
@bot.message_handler(func=lambda m: m.text == "💸 Перевод")
def transfer_start(m):
    try:
        if not sheet.find(str(m.from_user.id), in_column=2):
            return bot.send_message(m.chat.id, "❌ Вы не зарегистрированы.")
        msg = bot.send_message(m.chat.id, "Введите часть Ника игрока:")
        bot.register_next_step_handler(msg, search_recipient)
    except: bot.send_message(m.chat.id, "⚠️ Ошибка связи.")

def search_recipient(m):
    search_query = m.text.lower()
    try:
        all_rows = sheet.get_all_values()[1:]
        found = [p for p in all_rows if search_query in p[2].lower() and p[1] != str(m.from_user.id)]
        if not found: return bot.send_message(m.chat.id, "❌ Игроки не найдены.")
        
        kb = telebot.types.InlineKeyboardMarkup()
        for p in found[:10]:
            kb.add(telebot.types.InlineKeyboardButton(f"{p[2]} ({p[4]})", callback_data=f"tr_{p[1]}"))
        bot.send_message(m.chat.id, "Выберите получателя:", reply_markup=kb)
    except: bot.send_message(m.chat.id, "⚠️ Ошибка поиска.")

@bot.callback_query_handler(func=lambda c: c.data.startswith("tr_"))
def ask_amount(c):
    target_id = c.data.split("_")[1]
    u_data[c.from_user.id] = {'target_id': target_id}
    bot.delete_message(c.message.chat.id, c.message.message_id)
    msg = bot.send_message(c.message.chat.id, "Введите сумму перевода:")
    bot.register_next_step_handler(msg, confirm_transfer)

def confirm_transfer(m):
    try:
        amount = float(m.text.replace(',', '.'))
        if amount <= 0: return bot.send_message(m.chat.id, "❌ Сумма должна быть > 0.")
        
        s_cell = sheet.find(str(m.from_user.id), in_column=2)
        t_cell = sheet.find(u_data[m.from_user.id]['target_id'], in_column=2)
        
        s_row = sheet.row_values(s_cell.row)
        t_row = sheet.row_values(t_cell.row)
        
        s_bal = float(s_row[3].replace(',', '.'))
        t_bal = float(t_row[3].replace(',', '.'))
        
        if s_bal < amount: return bot.send_message(m.chat.id, "❌ Недостаточно Gold.")
        
        sheet.update_cell(s_cell.row, 4, str(s_bal - amount))
        sheet.update_cell(t_cell.row, 4, str(t_bal + amount))
        
        bot.send_message(m.chat.id, f"✅ Вы перевели {amount} Gold игроку {t_row[2]}.")
        bot.send_message(u_data[m.from_user.id]['target_id'], f"💰 Перевод!\n👤 От: {s_row[2]}\n➕ Сумма: {amount} Gold")
    except: bot.send_message(m.chat.id, "❌ Ошибка транзакции.")

# --- ПРОФИЛЬ И АДМИНКА ---
@bot.message_handler(func=lambda m: m.text == "👤 Мой профиль")
def show_profile(m):
    try:
        cell = sheet.find(str(m.from_user.id), in_column=2)
        if not cell: return bot.send_message(m.chat.id, "❌ Вы не зарегистрированы.")
        row = sheet.row_values(cell.row)
        text = (f"👤 **Профиль: {row[2]}**\n💼 Должность: {row[4]}\n💰 Баланс: **{row[3]} Gold**\n🆔 Код: `{row[0]}`")
        kb = telebot.types.InlineKeyboardMarkup().add(telebot.types.InlineKeyboardButton("📉 Снять Gold", callback_data="pre_withdraw"))
        bot.send_message(m.chat.id, text, parse_mode="Markdown", reply_markup=kb)
    except: bot.send_message(m.chat.id, "⚠️ Ошибка загрузки.")

@bot.message_handler(commands=['admin', 'config'])
@bot.message_handler(func=lambda m: m.text == "⚙️ Админ-панель")
def show_admin_panel(message):
    if not is_admin(message.from_user.id): return
    kb = telebot.types.InlineKeyboardMarkup()
    kb.add(telebot.types.InlineKeyboardButton("📊 Статистика", callback_data="adm_stats"))
    kb.add(telebot.types.InlineKeyboardButton("📢 Рассылка", callback_data="adm_broadcast"))
    kb.add(telebot.types.InlineKeyboardButton("💰 Изменить баланс", callback_data="adm_edit_bal"))
    bot.send_message(message.chat.id, "🛠 **Панель управления**", reply_markup=kb)

@bot.callback_query_handler(func=lambda c: True)
def cb_logic(c):
    if c.data == "adm_stats":
        count = len(sheet.get_all_values()) - 1
        bot.send_message(c.message.chat.id, f"📊 Игроков: {count}")
    elif c.data == "pre_withdraw":
        msg = bot.send_message(c.message.chat.id, "Сколько снять?")
        bot.register_next_step_handler(msg, send_withdraw_to_admin)
    elif c.data.startswith("confirm_"):
        _, r_idx, amt = c.data.split("_")
        finish_payout(c, int(r_idx), float(amt))
    bot.answer_callback_query(c.id)

def send_withdraw_to_admin(m):
    try:
        amt = float(m.text)
        cell = sheet.find(str(m.from_user.id), in_column=2)
        kb = telebot.types.InlineKeyboardMarkup().add(telebot.types.InlineKeyboardButton("✅ Одобрить", callback_data=f"confirm_{cell.row}_{amt}"))
        for a in ADMIN_LIST: bot.send_message(a, f"🚨 Заявка: {m.from_user.first_name} на {amt} Gold", reply_markup=kb)
        bot.send_message(m.chat.id, "⌛ Отправлено.")
    except: bot.send_message(m.chat.id, "❌ Ошибка.")

def finish_payout(c, r_idx, amt):
    try:
        row = sheet.row_values(r_idx)
        new_bal = float(row[3]) - amt
        sheet.update_cell(r_idx, 4, str(new_bal))
        bot.edit_message_text(f"✅ Выплачено {amt}", c.message.chat.id, c.message.message_id)
        bot.send_message(row[1], f"✅ Выплата {amt} Gold одобрена!")
    except: bot.send_message(c.message.chat.id, "❌ Ошибка.")

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

# --- 5. ЗАПУСК ---
app = Flask(__name__)
@app.route('/')
def h(): return "OK", 200

if __name__ == "__main__":
    Thread(target=lambda: app.run(host="0.0.0.0", port=8080), daemon=True).start()
    
    # Уведомление о запуске
    try:
        bot.send_message(NOTIFY_USER_ID, "🚀 **Бот запущен и готов к работе!**", parse_mode="Markdown")
    except Exception as e:
        print(f"Не удалось отправить уведомление о запуске: {e}")

    while True:
        try:
            bot.remove_webhook()
            bot.infinity_polling(none_stop=True, skip_pending=True)
        except: time.sleep(5)
