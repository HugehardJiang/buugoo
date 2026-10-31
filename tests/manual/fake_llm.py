# 模拟 OpenAI 兼容服务，用于本地手工测试（不花真实模型的钱）。
# 用法：python3 tests/manual/fake_llm.py [端口，默认 19090]
# 收到的请求会追加写到当前目录的 reqlog.jsonl，方便检查发给模型的提示词和工具。
import json, sys, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

WEATHER = """---
name: weather
description: 查询城市的实时天气。用户问天气、要不要带伞时使用。
---

# 查天气

运行 `python3 scripts/weather.py <城市>`，把输出整理后告诉用户。
"""

class H(BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        if self.path.endswith('/embeddings'):
            data = []
            for i, t in enumerate(body['input']):
                v = [0.05] + [1.0 if w in t else 0.0 for w in ['猫', '花生', '考试', '加班']]
                data.append({'index': i, 'embedding': v})
            out = json.dumps({'data': data}).encode()
            self.send_response(200); self.send_header('Content-Type', 'application/json'); self.end_headers(); self.wfile.write(out); return
        msgs = body['messages']
        tools = [t['function']['name'] for t in body.get('tools') or []]
        open('reqlog.jsonl', 'a').write(json.dumps({'tools': tools, 'msgs': body['messages']}, ensure_ascii=False) + '\n')
        # 当前这一轮：最后一条用户消息之后的工具调用次数。
        ui = max(i for i, m in enumerate(msgs) if m['role'] == 'user')
        utext = msgs[ui]['content'].split('\n')[-1] if isinstance(msgs[ui]['content'], str) else ''
        step = sum(1 for m in msgs[ui:] if m['role'] == 'tool')
        def call(name, args):
            return {'role': 'assistant', 'content': '', 'tool_calls': [{'id': f'c{step}', 'type': 'function', 'function': {'name': name, 'arguments': json.dumps(args, ensure_ascii=False)}}]}
        if 'get_my_settings' in tools and not body.get('stream'):
            plan = [{'role': 'assistant', 'content': '我是牛马，想改什么设置直接说。'}]
            if '昵称' in utext:
                plan = [call('get_my_settings', {}), call('set_display_name', {'name': '小明'}),
                        {'role': 'assistant', 'content': '你现在还没有昵称。我列了一项修改：把昵称改成「小明」，点下面的**确认**就生效。'}]
            elif '天气' in utext:
                plan = [call('list_skills', {}), call('install_skill', {'content': WEATHER}),
                        {'role': 'assistant', 'content': '现在还没有查天气的 skill。我准备装一个 **weather**，装好后所有人都能让布谷查天气，确认一下？'}]
            elif '压缩' in utext:
                plan = [call('search_settings', {'query': '压缩'}), call('change_settings', {'changes': {'chat.compact_at': 70}}),
                        {'role': 'assistant', 'content': '自动压缩阈值现在是 80%，改成 70% 需要你确认，保存后**重启服务**才生效。'}]
            msg = plan[min(step, len(plan) - 1)]
            out = json.dumps({'choices': [{'message': msg, 'finish_reason': 'tool_calls' if msg.get('tool_calls') else 'stop'}], 'usage': {'prompt_tokens': 300, 'completion_tokens': 40}}, ensure_ascii=False).encode()
            self.send_response(200); self.send_header('Content-Type', 'application/json'); self.end_headers(); self.wfile.write(out); return
        if body.get('stream') and 'todo_add' in tools and (utext.startswith('帮我记一下') or utext.startswith('我还有哪些没做') or '喊我' in utext or '没有调用 set_reminder' in utext):
            def sse(obj):
                self.wfile.write(('data: ' + json.dumps(obj, ensure_ascii=False) + '\n\n').encode()); self.wfile.flush()
            self.send_response(200); self.send_header('Content-Type', 'text/event-stream'); self.end_headers()
            def tc(name, args):
                sse({'choices': [{'delta': {'tool_calls': [{'index': 0, 'id': f'call_todo{step}', 'type': 'function', 'function': {'name': name, 'arguments': json.dumps(args, ensure_ascii=False)}}]}, 'finish_reason': 'tool_calls'}]})
            def say(t):
                for ch in t: sse({'choices': [{'delta': {'content': ch}}]}); time.sleep(0.005)
            if utext.startswith('帮我记一下'):
                item = utext[5:].strip('，,。 ')
                due = '2026-10-30 18:00' if '实验报告' in item else ''
                [lambda: tc('todo_add', {'title': item, 'due': due}), lambda: say('记下了～「' + item + '」' + ('，周五 18:00 截止，快到了我来问你' if due else ''))][min(step, 1)]()
            elif utext.startswith('我还有哪些没做'):
                def summary():
                    out = msgs[-1]['content']
                    lines = [l[2:] for l in out.splitlines() if l.startswith('- ')]
                    say('还有这几件：\n\n' + '\n'.join('- ' + l.split(' ', 1)[1] for l in lines) + '\n\n先把最急的实验报告搞定吧！')
                [lambda: tc('todo_list', {}), summary][min(step, 1)]()
            elif '没有调用 set_reminder' in utext:
                [lambda: tc('set_reminder', {'content': '赶大物慕课作业', 'time': '2026-10-29 15:30'}), lambda: None][min(step, 1)]()
            else:
                say('好，明天15:30喊你赶大物慕课作业')
            self.wfile.write(b'data: [DONE]\n\n'); return
        if body.get('stream') and 'memo_list' in tools and (utext.startswith('记到备忘录：') or utext.startswith('删掉备忘录里的') or utext == '确认删除'):
            import re
            def sse(obj):
                self.wfile.write(('data: ' + json.dumps(obj, ensure_ascii=False) + '\n\n').encode()); self.wfile.flush()
            self.send_response(200); self.send_header('Content-Type', 'text/event-stream'); self.end_headers()
            def tc(name, args):
                sse({'choices': [{'delta': {'tool_calls': [{'index': 0, 'id': f'call_memo{step}', 'type': 'function', 'function': {'name': name, 'arguments': json.dumps(args, ensure_ascii=False)}}]}, 'finish_reason': 'tool_calls'}]})
            def say(t):
                for ch in t: sse({'choices': [{'delta': {'content': ch}}]})
            if utext.startswith('记到备忘录：'):
                item = utext[6:]
                [lambda: tc('memo_list', {}), lambda: tc('memo_write', {'page': '购物清单', 'content': '- ' + item}), lambda: say('记好了，放在「购物清单」里')][min(step, 2)]()
            elif utext.startswith('删掉备忘录里的'):
                item = utext[7:]
                [lambda: tc('memo_delete', {'page': '购物清单', 'text': '- ' + item}), lambda: say('要把「' + item + '」从购物清单里删掉吗？')][min(step, 1)]()
            else:
                allc = json.dumps(msgs, ensure_ascii=False)
                m = re.findall(r'confirm_code=([0-9a-f]+)', allc)
                [lambda: tc('memo_delete', {'page': '购物清单', 'confirm_code': m[-1] if m else ''}), lambda: say('删好了')][min(step, 1)]()
            self.wfile.write(b'data: [DONE]\n\n'); return
        if body.get('stream') and utext.startswith('调插件 '):
            def sse(obj):
                self.wfile.write(('data: ' + json.dumps(obj, ensure_ascii=False) + '\n\n').encode()); self.wfile.flush()
            self.send_response(200); self.send_header('Content-Type', 'text/event-stream'); self.end_headers()
            name, args = utext[4:].split('|', 1)
            if step == 0:
                sse({'choices': [{'delta': {'tool_calls': [{'index': 0, 'id': 'call_m0', 'type': 'function', 'function': {'name': name, 'arguments': args}}]}, 'finish_reason': 'tool_calls'}]})
            else:
                sse({'choices': [{'delta': {'content': '插件返回了：' + msgs[-1]['content'][:60]}}]})
            self.wfile.write(b'data: [DONE]\n\n'); return
        if 'run_skill_script' in tools and body.get('stream') and utext.startswith('跑脚本 '):
            def sse(obj):
                self.wfile.write(('data: ' + json.dumps(obj, ensure_ascii=False) + '\n\n').encode()); self.wfile.flush()
            self.send_response(200); self.send_header('Content-Type', 'text/event-stream'); self.end_headers()
            name, cmd = utext[4:].split('|', 1)
            if step == 0:
                sse({'choices': [{'delta': {'tool_calls': [{'index': 0, 'id': 'call_r0', 'type': 'function', 'function': {'name': 'run_skill_script', 'arguments': json.dumps({'name': name, 'command': cmd}, ensure_ascii=False)}}]}, 'finish_reason': 'tool_calls'}]})
            else:
                for ch in '脚本跑完了。':
                    sse({'choices': [{'delta': {'content': ch}}]})
            self.wfile.write(b'data: [DONE]\n\n'); return
        if 'use_skill' in tools and body.get('stream') and '天气' in utext:
            def sse(obj):
                self.wfile.write(('data: ' + json.dumps(obj, ensure_ascii=False) + '\n\n').encode()); self.wfile.flush()
            self.send_response(200); self.send_header('Content-Type', 'text/event-stream'); self.end_headers()
            if step == 0:
                sse({'choices': [{'delta': {'tool_calls': [{'index': 0, 'id': 'call_s0', 'type': 'function', 'function': {'name': 'use_skill', 'arguments': json.dumps({'name': 'weather'})}}]}, 'finish_reason': 'tool_calls'}]})
            elif step == 1:
                sse({'choices': [{'delta': {'tool_calls': [{'index': 0, 'id': 'call_s1', 'type': 'function', 'function': {'name': 'run_skill_script', 'arguments': json.dumps({'name': 'weather', 'command': 'python3 scripts/weather.py 杭州'}, ensure_ascii=False)}}]}, 'finish_reason': 'tool_calls'}]})
            else:
                res = msgs[-1]['content']
                for ch in '查到了：' + res.strip().splitlines()[-1][:80]:
                    sse({'choices': [{'delta': {'content': ch}}]}); time.sleep(0.01)
            self.wfile.write(b'data: [DONE]\n\n'); return
        last = msgs[-1]['content']
        if isinstance(last, list):
            n = sum(1 for x in last if x.get('type') == 'image_url')
            last = ' '.join(x.get('text', '') for x in last) + f' [IMAGES:{n}]'
        if body.get('stream') and body.get('tools'):
            def sse(obj):
                self.wfile.write(('data: ' + json.dumps(obj, ensure_ascii=False) + '\n\n').encode()); self.wfile.flush()
            if msgs[-1]['role'] == 'tool':
                self.send_response(200); self.send_header('Content-Type', 'text/event-stream'); self.end_headers()
                out = msgs[-1]['content']
                for ch in '算好了：' + out.split('标准输出：')[-1].strip().splitlines()[0]:
                    sse({'choices': [{'delta': {'content': ch}}]}); time.sleep(0.01)
                self.wfile.write(b'data: [DONE]\n\n'); return
            if '画' in last:
                self.send_response(200); self.send_header('Content-Type', 'text/event-stream'); self.end_headers()
                sse({'choices': [{'delta': {'content': '我画给你看。'}}]})
                code = "import numpy as np, matplotlib.pyplot as plt\nx = np.arange(1, 8)\nplt.figure(figsize=(7, 4))\nplt.plot(x, [3, 5, 4, 6, 8, 7, 9], marker='o')\nplt.title('团子这周的饭量（勺）')\nplt.xlabel('星期')\nplt.grid(alpha=.3)\nprint('平均', np.mean([3,5,4,6,8,7,9]))"
                sse({'choices': [{'delta': {'tool_calls': [{'index': 0, 'id': 'call_1', 'type': 'function', 'function': {'name': 'run_python', 'arguments': json.dumps({'code': code})}}]}, 'finish_reason': 'tool_calls'}]})
                self.wfile.write(b'data: [DONE]\n\n'); return
            if '算' in last:
                self.send_response(200); self.send_header('Content-Type', 'text/event-stream'); self.end_headers()
                sse({'choices': [{'delta': {'content': '我用代码算一下。'}}]})
                time.sleep(0.3)
                sse({'choices': [{'delta': {'tool_calls': [{'index': 0, 'id': 'call_1', 'type': 'function', 'function': {'name': 'run_python', 'arguments': json.dumps({'code': 'from decimal import Decimal\nprint(Decimal(\'1.1\') * 3)\nprint(2**100)'})}}]}, 'finish_reason': 'tool_calls'}]})
                self.wfile.write(b'data: [DONE]\n\n'); return
        if '[IMAGES:' in last and body.get('stream'):
            self.send_response(200); self.send_header('Content-Type', 'text/event-stream'); self.end_headers()
            for ch in '我看到你发的图片啦！' + last[last.index('[IMAGES:'):]:
                self.wfile.write(('data: ' + json.dumps({'choices': [{'delta': {'content': ch}}]}, ensure_ascii=False) + '\n\n').encode()); self.wfile.flush()
            self.wfile.write(b'data: [DONE]\n\n'); return
        if body.get('response_format'):
            mems = []
            if '用户：我养了一只猫' in last:
                mems.append({'type': 'semantic', 'content': '用户养了一只叫团子的橘猫', 'importance': 7, 'emotion': 0.4, 'source': 'stated'})
            if '用户：最近天天加班' in last:
                mems.append({'type': 'episodic', 'content': '用户 10 月下旬工作很忙，天天加班', 'importance': 6, 'emotion': 0.6, 'source': 'stated'})
                mems.append({'type': 'semantic', 'content': '用户可能对现在的工作强度不太满意', 'importance': 5, 'emotion': 0.4, 'source': 'inferred'})
            appr = {'pleasantness': 0.5, 'respect': 0.5, 'care': 0.3, 'user_emotion': 'joy', 'user_intensity': 0.4, 'cause': '聊得挺开心'}
            if '用户：你真笨' in last:
                appr = {'pleasantness': -0.8, 'respect': -0.9, 'user_emotion': 'anger', 'user_intensity': 0.7, 'cause': '对方说我真笨'}
            out = json.dumps({'choices': [{'message': {'content': json.dumps({'memories': mems, 'appraisal': appr}, ensure_ascii=False)}}], 'usage': {'prompt_tokens': 50, 'completion_tokens': 20}}).encode()
            self.send_response(200); self.send_header('Content-Type', 'application/json'); self.end_headers(); self.wfile.write(out); return
        if not body.get('stream'):
            out = json.dumps({'choices': [{'message': {'content': '你好呀，我在。'}}], 'usage': {'prompt_tokens': 5, 'completion_tokens': 5}}, ensure_ascii=False).encode()
            self.send_response(200); self.send_header('Content-Type', 'application/json'); self.end_headers(); self.wfile.write(out); return
        if '积分' in last:
            reply = '选 **A**。\n\n参数化：取 \\(z=e^{i\\theta}\\)（\\(0\\le\\theta\\le2\\pi\\)），那么\n\n\\[\n\\int_C y\\,dz=\\int_0^{2\\pi}\\sin\\theta\\cdot ie^{i\\theta}\\,d\\theta=-\\pi\n\\]\n\n花了 $5 和 $10，行内也可以 $x^2+y^2=1$。'
        elif False:
            pass
        reply = reply if '积分' in last else '诶，团子最近还好吗？' if '猫' in last else '辛苦啦。加班这么多，**晚饭**有好好吃吗？\n\n别忘了周末找朋友出去走走。'
        if '裸公式' in last:
            reply = '答案用 LaTeX 写就是\n\\frac{\\sin at - at\\cos at}{2a^{3}},\\quad t\\ge 0'
        if '今天吃什么' in last:
            H.n = getattr(H, 'n', 0) + 1
            reply = ['吃火锅吧，天冷了正合适！', '要不来碗热乎乎的牛肉面？', '试试楼下新开的那家麻辣烫～'][(H.n - 1) % 3]
        slow = '慢慢说' in last
        if slow:
            reply = '好的，我慢慢说。' + '这是一段很长的回复，' * 6 + '说完啦。'
        self.send_response(200); self.send_header('Content-Type', 'text/event-stream'); self.end_headers()
        if body.get('reasoning_effort') or body.get('model') == 'fake-reasoner':
            for ch in '用户提到了猫。我记得团子是一只橘猫，不吃东西可能是天气变化或者换了猫粮。先关心一下，再给点建议。':
                self.wfile.write(('data: ' + json.dumps({'choices': [{'delta': {'reasoning_content': ch}}]}, ensure_ascii=False) + '\n\n').encode()); self.wfile.flush(); time.sleep(0.06)
        for ch in reply:
            self.wfile.write(('data: ' + json.dumps({'choices': [{'delta': {'content': ch}}]}, ensure_ascii=False) + '\n\n').encode()); self.wfile.flush(); time.sleep(0.06 if slow else 0.01)
        self.wfile.write(b'data: {"choices":[],"usage":{"prompt_tokens":800,"completion_tokens":30,"prompt_cache_hit_tokens":600,"prompt_cache_miss_tokens":200}}\n\ndata: [DONE]\n\n')

ThreadingHTTPServer(('127.0.0.1', int(sys.argv[1]) if len(sys.argv) > 1 else 19090), H).serve_forever()
