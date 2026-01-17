import os
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

# Создаем файл ключей
if GCP_JSON_DATA:
    with open("credentials.json", "w") as f:
        f.write(GCP_JSON_DATA)

ADMIN_LIST = [7631664265, 6343896085]

# --- 2. ПОДКЛЮЧЕНИЕ ТАБЛИЦ ---
scope = ["https://spreadsheets.google.com/feeds", "https://www.googleapis.com/auth/drive"]
creds = ServiceAccountCredentials.from_json_keyfile_name("credentials.json", scope)
gc = gspread.authorize(creds)
main_doc = gc.open(SHEET_NAME)
sheet = main_doc.sheet1
history_sheet = main_doc.worksheet("История")

bot = telebot.TeleBot(BOT_TOKEN)
u_data = {}

# --- 3. ЛОГИКА (Регистрация, Баланс, Вывод) ---
def gen_id():
    while True:
        new_id = ''.join(random.choice(string.ascii_letters + string.digits) for _ in range(12))
        if not sheet.find(new_id, in_column=1): return new_id

@bot.message_handler(commands=['start'])
def welcome(message):
    markup = telebot.types.ReplyKeyboardMarkup(resize_keyboard=True, row_width=2)
    markup.add("📝 Регистрация", "💰 Баланс", "📉 Снять монеты")
    bot.send_message(message.chat.id, "Выберите действие:", reply_markup=markup)

@bot.message_handler(func=lambda m: m.text == "📝 Регистрация")
def reg(m):
    if sheet.find(str(m.from_user.id), in_column=2):
        return bot.send_message(m.chat.id, "❌ Вы уже зарегистрированы!")
    msg = bot.send_message(m.chat.id, "Введите ваш Ник:")
    bot.register_next_step_handler(msg, get_nick)

def get_nick(m):
    u_data[m.from_user.id] = {'n': m.text}
    msg = bot.send_message(m.chat.id, "Введите вашу Должность:")
    bot.register_next_step_handler(msg, get_job)

def get_job(m):
    uid = m.from_user.id
    if uid not in u_data: return
    pwd = gen_id()
    sheet.append_row([pwd, str(uid), u_data[uid]['n'], 0, m.text])
    bot.send_message(m.chat.id, f"✅ Готово! Ваш ID: `{pwd}`", parse_mode="Markdown")

@bot.message_handler(func=lambda m: m.text == "💰 Баланс")
def ask_bal(m):
    msg = bot.send_message(m.chat.id, "🔐 Введите ваш 12-значный ID:")
    bot.register_next_step_handler(msg, show_bal)

def show_bal(m):
    try:
        row = sheet.row_values(sheet.find(m.text.strip(), in_column=1).row)
        bot.send_message(m.chat.id, f"👤 {row[2]}\n💰 {row[3]} Gold.")
    except:
        bot.send_message(m.chat.id, "❌ ID не найден.")

@bot.message_handler(func=lambda m: m.text == "📉 Снять монеты")
def with_start(m):
    msg = bot.send_message(m.chat.id, "Сколько монет снять?")
    bot.register_next_step_handler(msg, proc_with)

def proc_with(m):
    try:
        amt = float(m.text.replace(',', '.'))
        cell = sheet.find(str(m.from_user.id), in_column=2)
        row = sheet.row_values(cell.row)
        bal = float(str(row[3]).replace(',', '.'))
        
        if bal < amt: return bot.send_message(m.chat.id, "❌ Недостаточно средств.")

        kb = telebot.types.InlineKeyboardMarkup().add(
            telebot.types.InlineKeyboardButton("✅ Да", callback_data=f"ok_{cell.row}_{amt}"),
            telebot.types.InlineKeyboardButton("❌ Нет", callback_data="no")
        )
        for adm in ADMIN_LIST:
            bot.send_message(adm, f"🚨 Заявка: {row[2]} — {amt} Gold", reply_markup=kb)
        bot.send_message(m.chat.id, "⌛ Заявка отправлена администраторам.")
    except:
        bot.send_message(m.chat.id, "❌ Ошибка данных.")

@bot.callback_query_handler(func=lambda c: True)
def cb(c):
    if c.data.startswith("ok_"):
        _, r_idx, amt = c.data.split("_")
        r_idx, amt = int(r_idx), float(amt)
        row = sheet.row_values(r_idx)
        
        # Обновляем баланс
        new_bal = float(str(row[3]).replace(',', '.')) - amt
        sheet.update_cell(r_idx, 4, str(new_bal))
        
        # Записываем в историю
        history_sheet.append_row([datetime.now().strftime("%d.%m %H:%M"), row[2], c.from_user.first_name, amt])
        
        bot.edit_message_text(f"✅ Выплачено {amt} Gold", c.message.chat.id, c.message.message_id)
        bot.send_message(row[1], f"✅ Ваша выплата {amt} Gold подтверждена!")
    else:
        bot.edit_message_text("❌ Отклонено", c.message.chat.id, c.message.message_id)

# --- 4. ВЕБ-СЕРВЕР ---
app = Flask(__name__)
@app.route('/')
def health(): return "Status: OK"

# --- 5. ЗАПУСК ---
if __name__ == "__main__":
    # Запускаем Flask на порту 8080 (стандарт для Koyeb)
    Thread(target=lambda: app.run(host="0.0.0.0", port=8080), daemon=True).start()
    print("🚀 Бот запущен...")
    bot.infinity_polling(none_stop=True)