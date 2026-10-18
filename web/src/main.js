// 画面の入口。App を #app に組み付けるだけ
import { mount } from 'svelte';
import './app.css';
import App from './App.svelte';

mount(App, { target: document.getElementById('app') });
