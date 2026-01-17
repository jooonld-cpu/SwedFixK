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

# Список ID администраторов (получают заявки на вывод)
ADMIN_LIST = [7631664265, 6343896085]

# Создаем файл ключей
if GCP_JSON_DATA:
    try:
        with open("credentials.json", "w") as f:
            f.write(GCP_JSON_DATA)
        print("✅ Файл credentials.json создан", flush=True)
    except Exception as e:
        print(f"❌ Ошибка записи JSON: {e}", flush=True)

# --- 2. ПОДКЛЮЧЕНИЕ К ТАБЛИЦАМ ---
try:
    print(f"📡 Подключение к Google Таблице: {SHEET_NAME}...", flush=True)
    scope = ["https://spreadsheets.google.com/feeds", "https://www.googleapis.com/auth/drive"]
    creds = ServiceAccountCredentials.from_json_keyfile_name("credentials.json", scope)
    gc = gspread.authorize(creds)
    main_doc = gc.open(SHEET_NAME)
    sheet = main_doc.sheet1
    
    # Пытаемся найти или создать лист истории
    try:
        history_sheet = main_doc.worksheet("История")
    except:
        history_sheet = main_doc.add_worksheet(title="История", rows="1000", cols="5")
        history_sheet.append_row(["Дата", "Ник", "Админ", "Сумма"])
        
    print("✅ Таблицы успешно подключены!", flush=True)
except Exception as e:
    print(f"❌ ОШИБКА ТАБЛИЦ: {e}", flush=True)

bot = telebot.TeleBot(BOT_TOKEN)
u_data = {}

# --- 3. ФУНКЦИИ ---
def gen_id():
    """Генерирует уникальный 12-значный ID"""
    while True:
        new_id = ''.join(random.choice(string.ascii_letters + string.digits) for _ in range(12))
        if not sheet.find(new_id, in_column=1):
            return new_id

# --- 4. ОБРАБОТЧИКИ ---

@bot.message_handler(commands=['start'])
def welcome(message):
    print(f"➡️ Пользователь {message.from_user.id} нажал /start", flush=True)
    markup = telebot.types.ReplyKeyboardMarkup(resize_keyboard=True, row_width=2)
    markup.add("📝 Регистрация", "💰 Баланс", "📉 Снять монеты")
    bot.send_message(message.chat.id, "👋 Добро пожаловать! Выберите действие на клавиатуре:", reply_markup=markup)

# --- РЕГИСТРАЦИЯ ---
@bot.message_handler(func=lambda m: m.text == "📝 Регистрация")
def reg(m):
    print(f"➡️ Нажата Регистрация ({m.from_user.id})", flush=True)
    try:
        if sheet.find(str(m.from_user.id), in_column=2):
            return bot.send_message(m.chat.id, "❌ Вы уже зарегистрированы в системе!")
        msg = bot.send_message(m.chat.id, "Введите ваш Ник (имя в игре):")
        bot.register_next_step_handler(msg, get_nick)
    except Exception as e:
        print(f"❌ Ошибка в reg: {e}", flush=True)

def get_nick(m):
    u_data[m.from_user.id] = {'n': m.text}
    msg = bot.send_message(m.chat.id, "Введите вашу Должность:")
    bot.register_next_step_handler(msg, get_job)

def get_job(m):
    uid = m.from_user.id
    if uid not in u_data: return
    try:
        pwd = gen_id()
        # Столбцы: ID(1), TG_ID(2), Nick(3), Balance(4), Job(5)
        sheet.append_row([pwd, str(uid), u_data[uid]['n'], "0", m.text])
        bot.send_message(m.chat.id, f"✅ Регистрация завершена!\n🔑 Ваш личный ID: `{pwd}`\n\nНикому не сообщайте этот код!", parse_mode="Markdown")
        print(f"✅ Новый пользователь: {u_data[uid]['n']}", flush=True)
    except Exception as e:
        bot.send_message(m.chat.id, "❌ Ошибка при записи в таблицу.")
        print(f"❌ Ошибка в get_job: {e}", flush=True)

# --- БАЛАНС ---
@bot.message_handler(func=lambda m: m.text == "💰 Баланс")
def ask_bal(m):
    msg = bot.send_message(m.chat.id, "🔐 Введите ваш 12-значный ID для проверки баланса:")
    bot.register_next_step_handler(msg, show_bal)

def show_bal(m):
    try:
        user_code = m.text.strip()
        cell = sheet.find(user_code, in_column=1)
        if cell:
            row = sheet.row_values(cell.row)
            bot.send_message(m.chat.id, f"👤 Игрок: {row[2]}\n💰 Баланс: {row[3]} Gold")
        else:
            bot.send_message(m.chat.id, "❌ Код не найден. Проверьте правильность ввода.")
    except Exception as e:
        bot.send_message(m.chat.id, "⚠️ Ошибка при поиске.")
        print(f"❌ Ошибка в show_bal: {e}", flush=True)

# --- ВЫВОД СРЕДСТВ ---
@bot.message_handler(func=lambda m: m.text == "📉 Снять монеты")
def with_start(m):
    print(f"➡️ Нажато Снятие ({m.from_user.id})", flush=True)
    try:
        cell = sheet.find(str(m.from_user.id), in_column=2)
        if not cell:
            return bot.send_message(m.chat.id, "❌ Вы не зарегистрированы. Сначала пройдите регистрацию.")
        
        msg = bot.send_message(m.chat.id, "Сколько Gold вы хотите снять?")
        bot.register_next_step_handler(msg, proc_with)
    except Exception as e:
        print(f"❌ Ошибка в with_start: {e}", flush=True)

def proc_with(m):
    try:
        # Валидация числа
        amount_txt = m.text.replace(',', '.')
        if not amount_txt.replace('.', '', 1).isdigit():
            return bot.send_message(m.chat.id, "❌ Введите только число (например: 100 или 50.5)")
        
        amt = float(amount_txt)
        cell = sheet.find(str(m.from_user.id), in_column=2)
        row = sheet.row_values(cell.row)
        
        # Проверка баланса (4-й столбец)
        balance = float(str(row[3]).replace(',', '.'))
        
        if balance < amt:
            return bot.send_message(m.chat.id, f"❌ Недостаточно средств. Ваш баланс: {balance} Gold")

        # Кнопки для админов
        kb = telebot.types.InlineKeyboardMarkup()
        kb.add(
            telebot.types.InlineKeyboardButton("✅ Одобрить", callback_data=f"ok_{cell.row}_{amt}"),
            telebot.types.InlineKeyboardButton("❌ Отказать", callback_data="no")
        )
        
        for adm in ADMIN_LIST:
            try:
                bot.send_message(adm, f"🚨 ЗАЯВКА НА ВЫВОД\n👤 От: {row[2]}\n💰 Сумма: {amt} Gold", reply_markup=kb)
            except: pass
            
        bot.send_message(m.chat.id, "⌛ Заявка отправлена администраторам. Вы получите уведомление о результате.")
    except Exception as e:
        bot.send_message(m.chat.id, "⚠️ Ошибка при создании заявки.")
        print(f"❌ Ошибка в proc_with: {e}", flush=True)

@bot.callback_query_handler(func=lambda c: True)
def cb_inline(c):
    if c.data.startswith("ok_"):
        _, r_idx, amt = c.data.split("_")
        r_idx, amt = int(r_idx), float(amt)
        
        try:
            row = sheet.row_values(r_idx)
            old_bal = float(str(row[3]).replace(',', '.'))
            new_bal = old_bal - amt
            
            # Обновляем в таблице
            sheet.update_cell(r_idx, 4, str(new_bal))
            # В историю
            history_sheet.append_row([datetime.now().strftime("%d.%m %H:%M"), row[2], c.from_user.first_name, amt])
            
            bot.edit_message_text(f"✅ Выплачено {amt} Gold игроку {row[2]}", c.message.chat.id, c.message.message_id)
            bot.send_message(row[1], f"✅ Ваша заявка на {amt} Gold одобрена! Баланс обновлен.")
        except Exception as e:
            bot.send_message(c.message.chat.id, f"❌ Ошибка БД: {e}")
    elif c.data == "no":
        bot.edit_message_text("❌ Заявка отклонена администратором", c.message.chat.id, c.message.message_id)

# --- 5. ВЕБ-СЕРВЕР (ДЛЯ KOYEB) ---
app = Flask(__name__)
@app.route('/')
def health(): return "OK", 200

def run_web():
    app.run(host="0.0.0.0", port=8080)

# --- 6. ЗАПУСК ---
if __name__ == "__main__":
    Thread(target=run_web, daemon=True).start()
    
    while True:
        try:
            print("🧹 Сброс Webhook и запуск Polling...", flush=True)
            bot.remove_webhook()
            # skip_pending=True чтобы не спамить старыми сообщениями при рестарте
            bot.infinity_polling(none_stop=True, skip_pending=True, timeout=60)
        except Exception as e:
            print(f"❌ Ошибка цикла: {e}", flush=True)
            time.sleep(10)
