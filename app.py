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

# --- 1. НАСТРОЙКИ (БЕРЕМ ИЗ ПЕРЕМЕННЫХ ОКРУЖЕНИЯ) ---
BOT_TOKEN = os.getenv("BOT_TOKEN")
SHEET_NAME = os.getenv("SHEET_NAME", "SwedenFINK")
GCP_JSON_DATA = os.getenv("GCP_JSON")

# Создаем файл ключей из переменной
if GCP_JSON_DATA:
    try:
        with open("credentials.json", "w") as f:
            f.write(GCP_JSON_DATA)
        print("✅ Файл credentials.json успешно создан")
    except Exception as e:
        print(f"❌ Ошибка создания JSON: {e}")

ADMIN_LIST = [7631664265, 6343896085]

# --- 2. ПОДКЛЮЧЕНИЕ К GOOGLE TABLES ---
try:
    print(f"📡 Подключение к таблице '{SHEET_NAME}'...")
    scope = ["https://spreadsheets.google.com/feeds", "https://www.googleapis.com/auth/drive"]
    creds = ServiceAccountCredentials.from_json_keyfile_name("credentials.json", scope)
    gc = gspread.authorize(creds)
    main_doc = gc.open(SHEET_NAME)
    sheet = main_doc.sheet1
    history_sheet = main_doc.worksheet("История")
    print("✅ Google Tables успешно подключены!")
except Exception as e:
    print(f"❌ КРИТИЧЕСКАЯ ОШИБКА ТАБЛИЦ: {e}")

bot = telebot.TeleBot(BOT_TOKEN)
u_data = {}

# --- 3. ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ ---
def gen_id():
    while True:
        new_id = ''.join(random.choice(string.ascii_letters + string.digits) for _ in range(12))
        if not sheet.find(new_id, in_column=1): return new_id

# --- 4. ОБРАБОТЧИКИ КОМАНД ---

@bot.message_handler(commands=['start'])
def welcome(message):
    print(f"➡️ Команда /start от {message.from_user.id}", flush=True)
    markup = telebot.types.ReplyKeyboardMarkup(resize_keyboard=True, row_width=2)
    markup.add("📝 Регистрация", "💰 Баланс", "📉 Снять монеты")
    bot.send_message(message.chat.id, "👋 Добро пожаловать! Выберите действие:", reply_markup=markup)

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
    bot.send_message(m.chat.id, f"✅ Регистрация успешна!\n🔑 Ваш ID: `{pwd}`", parse_mode="Markdown")

@bot.message_handler(func=lambda m: m.text == "💰 Баланс")
def ask_bal(m):
    msg = bot.send_message(m.chat.id, "🔐 Введите ваш 12-значный ID:")
    bot.register_next_step_handler(msg, show_bal)

def show_bal(m):
    try:
        cell = sheet.find(m.text.strip(), in_column=1)
        row = sheet.row_values(cell.row)
        bot.send_message(m.chat.id, f"👤 Пользователь: {row[2]}\n💰 Баланс: {row[3]} Gold.")
    except:
        bot.send_message(m.chat.id, "❌ ID не найден. Проверьте правильность ввода.")

@bot.message_handler(func=lambda m: m.text == "📉 Снять монеты")
def with_start(m):
    msg = bot.send_message(m.chat.id, "Введите сумму для снятия:")
    bot.register_next_step_handler(msg, proc_with)

def proc_with(m):
    try:
        amt = float(m.text.replace(',', '.'))
        cell = sheet.find(str(m.from_user.id), in_column=2)
        row = sheet.row_values(cell.row)
        bal = float(str(row[3]).replace(',', '.'))
        
        if bal < amt: return bot.send_message(m.chat.id, "❌ Недостаточно Gold на балансе.")

        kb = telebot.types.InlineKeyboardMarkup().add(
            telebot.types.InlineKeyboardButton("✅ Одобрить", callback_data=f"ok_{cell.row}_{amt}"),
            telebot.types.InlineKeyboardButton("❌ Отклонить", callback_data="no")
        )
        for adm in ADMIN_LIST:
            bot.send_message(adm, f"🚨 Заявка на вывод:\n👤 Кто: {row[2]}\n💰 Сумма: {amt} Gold", reply_markup=kb)
        bot.send_message(m.chat.id, "⌛ Заявка отправлена администраторам. Ожидайте подтверждения.")
    except:
        bot.send_message(m.chat.id, "❌ Ошибка. Убедитесь, что вы зарегистрированы и ввели число.")

@bot.callback_query_handler(func=lambda c: True)
def cb(c):
    if c.data.startswith("ok_"):
        _, r_idx, amt = c.data.split("_")
        r_idx, amt = int(r_idx), float(amt)
        row = sheet.row_values(r_idx)
        
        new_bal = float(str(row[3]).replace(',', '.')) - amt
        sheet.update_cell(r_idx, 4, str(new_bal))
        
        history_sheet.append_row([datetime.now().strftime("%d.%m %H:%M"), row[2], c.from_user.first_name, amt])
        
        bot.edit_message_text(f"✅ Выплачено {amt} Gold пользователю {row[2]}", c.message.chat.id, c.message.message_id)
        bot.send_message(row[1], f"✅ Ваша заявка на {amt} Gold одобрена!")
    elif c.data == "no":
        bot.edit_message_text("❌ Заявка отклонена", c.message.chat.id, c.message.message_id)

# --- 5. ВЕБ-СЕРВЕР ДЛЯ KOYEB (HEALTH CHECK) ---
server = Flask(__name__)
@server.route('/')
def health(): return "I am alive!", 200

def run_flask():
    server.run(host="0.0.0.0", port=8080)

# --- 6. ЗАПУСК ПРИЛОЖЕНИЯ ---
if __name__ == "__main__":
    # Запуск Flask в отдельном потоке
    Thread(target=run_flask, daemon=True).start()
    
    time.sleep(2) # Даем серверу запуститься
    
    while True:
        try:
            print("🧹 Очистка Webhook...", flush=True)
            bot.remove_webhook()
            print("🚀 Запуск Polling...", flush=True)
            # skip_pending=True игнорирует сообщения, присланные пока бот был выключен
            bot.infinity_polling(none_stop=True, skip_pending=True, timeout=60)
        except Exception as e:
            print(f"⚠️ Ошибка polling: {e}. Перезапуск через 10 сек...")
            time.sleep(10)
