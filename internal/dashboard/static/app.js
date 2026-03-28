async function fetchStatus() {
    const response = await fetch('/api/status');
    const nodes = await response.json();
    updateUI(nodes);
}

function updateUI(nodes) {
    const grid = document.getElementById('node-grid');
    grid.innerHTML = '';

    let aliveCount = 0;
    let totalCount = 0;

    for (const [addr, status] of Object.entries(nodes)) {
        totalCount++;
        if (status.alive) aliveCount++;

        const card = document.createElement('div');
        card.className = `node-card ${status.alive ? 'alive' : ''}`;
        card.innerHTML = `
            <h4>Node: ${status.id}</h4>
            <p>Address: ${addr}</p>
            <div class="status-badge">${status.alive ? 'ONLINE' : 'OFFLINE'}</div>
        `;
        grid.appendChild(card);
    }

    document.getElementById('active-count').innerText = `${aliveCount} / ${totalCount}`;
    
    const f = Math.floor((totalCount - 1) / 3);
    const quorum = 2 * f + 1;
    const quorumStatus = document.getElementById('quorum-status');
    
    if (aliveCount >= quorum) {
        quorumStatus.innerText = `READY (${quorum} needed)`;
        quorumStatus.style.color = '#03dac6';
    } else {
        quorumStatus.innerText = `LACKING (${quorum} needed)`;
        quorumStatus.style.color = '#cf6679';
    }
}

setInterval(fetchStatus, 2000);
fetchStatus();
