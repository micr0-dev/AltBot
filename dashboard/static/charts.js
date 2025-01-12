let charts = {};

async function fetchMetrics() {
    const response = await fetch('/api/metrics');
    const data = await response.json();
    return data;
}

function processMetrics(data) {
    const eventCounts = {};
    const userSet = new Set();
    const timelineData = {};

    data.forEach(event => {
        // Count events
        eventCounts[event.EventType] = (eventCounts[event.EventType] || 0) + 1;

        // Collect unique users
        userSet.add(event.UserID);

        // Timeline data
        const date = event.Timestamp.split('T')[0];
        if (!timelineData[date]) timelineData[date] = {};
        timelineData[date][event.EventType] = (timelineData[date][event.EventType] || 0) + 1;
    });

    return {
        eventCounts,
        uniqueUsers: userSet.size,
        timelineData
    };
}

function updateCharts(metrics) {
    // Event Distribution Pie Chart
    const ctx1 = document.getElementById('eventsPie').getContext('2d');
    if (charts.eventsPie) charts.eventsPie.destroy();
    charts.eventsPie = new Chart(ctx1, {
        type: 'pie',
        data: {
            labels: Object.keys(metrics.eventCounts),
            datasets: [{
                data: Object.values(metrics.eventCounts),
                backgroundColor: [
                    '#FF6384',
                    '#36A2EB',
                    '#FFCE56',
                    '#4BC0C0',
                    '#9966FF'
                ]
            }]
        }
    });

    // Update quick stats
    document.getElementById('totalUsers').textContent = metrics.uniqueUsers;
    document.getElementById('totalRequests').textContent = metrics.eventCounts.request || 0;
    document.getElementById('rateLimits').textContent = metrics.eventCounts.rate_limit_hit || 0;

    // Update last updated time
    document.getElementById('lastUpdated').textContent = new Date().toLocaleString();
}

async function updateDashboard() {
    const metrics = await fetchMetrics();
    const processed = processMetrics(metrics);
    updateCharts(processed);
}

// Initial load
updateDashboard();

// Refresh every minute
setInterval(updateDashboard, 60000);