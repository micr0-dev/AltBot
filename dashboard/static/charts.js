let charts = {};

async function fetchMetrics() {
    const response = await fetch('/api/metrics');
    const data = await response.json();
    return data;
}

// Add this helper function at the top of your charts.js
function isDarkMode() {
    return window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches;
}

// Create a chart options helper
function getChartDefaults() {
    const textColor = isDarkMode() ? '#f1f5f9' : '#1e293b';
    return {
        color: textColor,
        plugins: {
            legend: {
                labels: {
                    color: textColor
                }
            }
        },
        scales: {
            x: {
                grid: {
                    display: false
                },
                ticks: {
                    color: textColor
                }
            },
            y: {
                grid: {
                    display: false
                },
                ticks: {
                    color: textColor
                }
            }
        }
    };
}

function processMetrics(data) {
    const eventCounts = {};
    const mediaTypeCounts = {};
    const userSet = new Set();
    const imageResponseTimes = [];
    const hourlyActivity = Array(24).fill(0);

    data.forEach(event => {
        // Count events
        eventCounts[event.EventType] = (eventCounts[event.EventType] || 0) + 1;

        // Collect unique users
        userSet.add(event.UserID);

        // Process successful generations
        if (event.EventType === 'successful_generation' && event.Details) {
            const mediaType = event.Details.mediaType;
            mediaTypeCounts[mediaType] = (mediaTypeCounts[mediaType] || 0) + 1;

            if (mediaType === 'image' && event.Details.responseTime) {
                imageResponseTimes.push({
                    timestamp: new Date(event.Timestamp),
                    responseTime: event.Details.responseTime
                });
            }
        }

        // Hourly activity
        const hour = new Date(event.Timestamp).getHours();
        hourlyActivity[hour]++;
    });

    // Calculate average response time
    const avgResponseTime = imageResponseTimes.length > 0
        ? Math.round(imageResponseTimes.reduce((acc, curr) => acc + curr.responseTime, 0) / imageResponseTimes.length)
        : 0;

    const userEngagement = {
        dates: {},
        newUsers: {},
        activeUsers: {}
    };

    data.forEach(event => {
        const date = new Date(event.Timestamp).toISOString().split('T')[0];
        if (!userEngagement.dates[date]) {
            userEngagement.dates[date] = new Set();
            userEngagement.newUsers[date] = new Set();
        }
        userEngagement.dates[date].add(event.UserID);

        // Check if this is user's first activity
        const userFirstActivity = data
            .find(e => e.UserID === event.UserID)
            .Timestamp.split('T')[0];
        if (userFirstActivity === date) {
            userEngagement.newUsers[date].add(event.UserID);
        }
    });

    // Convert Sets to counts
    Object.keys(userEngagement.dates).forEach(date => {
        userEngagement.activeUsers[date] = userEngagement.dates[date].size;
        userEngagement.newUsers[date] = userEngagement.newUsers[date].size;
    });

    return {
        eventCounts,
        mediaTypeCounts,
        uniqueUsers: userSet.size,
        imageResponseTimes,
        hourlyActivity,
        avgResponseTime,
        userEngagement
    };
}

function updateCharts(metrics) {
    const defaultOptions = getChartDefaults();

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
                    '#6366f1',
                    '#8b5cf6',
                    '#d946ef',
                    '#ec4899',
                    '#f43f5e'
                ]
            }]
        },
        options: {
            ...defaultOptions,
            plugins: {
                legend: {
                    display: false  // This removes the legend
                }
            }
        }
    });

    // Media Type Distribution Pie Chart
    const ctx2 = document.getElementById('mediaTypePie').getContext('2d');
    if (charts.mediaTypePie) charts.mediaTypePie.destroy();
    charts.mediaTypePie = new Chart(ctx2, {
        type: 'pie',
        data: {
            labels: Object.keys(metrics.mediaTypeCounts),
            datasets: [{
                data: Object.values(metrics.mediaTypeCounts),
                backgroundColor: [
                    '#6366f1',
                    '#8b5cf6',
                    '#d946ef'
                ]
            }]
        },
        options: {
            ...defaultOptions,
            plugins: {
                legend: {
                    position: 'bottom',
                    labels: {
                        color: defaultOptions.color,
                        padding: 20
                    }
                }
            }
        }
    });

    const ctx3 = document.getElementById('combinedChart').getContext('2d');
    if (charts.combinedChart) charts.combinedChart.destroy();

    // Calculate average response times per hour
    const hourlyResponseTimes = Array(24).fill(0).map(() => ({ sum: 0, count: 0 }));
    metrics.imageResponseTimes.forEach(item => {
        const hour = new Date(item.timestamp).getHours();
        hourlyResponseTimes[hour].sum += item.responseTime;
        hourlyResponseTimes[hour].count++;
    });

    const avgResponseTimes = hourlyResponseTimes.map(h =>
        h.count > 0 ? h.sum / h.count : 0);

    charts.combinedChart = new Chart(ctx3, {
        type: 'line',
        data: {
            labels: Array.from({ length: 24 }, (_, i) => `${i}:00`),
            datasets: [{
                label: 'Activity',
                data: metrics.hourlyActivity,
                type: 'bar',
                backgroundColor: 'rgba(99, 102, 241, 0.5)',
                borderRadius: 4,
                yAxisID: 'y1'
            }, {
                label: 'Avg Response Time (ms)',
                data: avgResponseTimes,
                borderColor: '#ef4444',
                tension: 0.4,
                fill: false,
                yAxisID: 'y2'
            }]
        },
        options: {
            ...defaultOptions,
            maintainAspectRatio: true,
            aspectRatio: 2,
            scales: {
                y1: {
                    type: 'linear',
                    position: 'left',
                    grid: {
                        display: false
                    },
                    ticks: {
                        color: defaultOptions.color
                    }
                },
                y2: {
                    type: 'linear',
                    position: 'right',
                    grid: {
                        display: false
                    },
                    ticks: {
                        color: defaultOptions.color
                    }
                }
            }
        }
    });

    // Update stats
    document.getElementById('totalRequests').textContent =
        Object.values(metrics.eventCounts).reduce((a, b) => a + b, 0);
    document.getElementById('totalUsers').textContent = metrics.uniqueUsers;
    document.getElementById('avgResponseTime').textContent = `${metrics.avgResponseTime}ms`;
    document.getElementById('rateLimits').textContent =
        metrics.eventCounts.rate_limit_hit || 0;

    document.getElementById('lastUpdated').textContent =
        new Date().toLocaleString();
}

// Add dark mode listener
window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
    updateDashboard();
});

async function updateDashboard() {
    const metrics = await fetchMetrics();
    const processed = processMetrics(metrics);
    updateCharts(processed);
    updateTimeline(metrics);
}

// Initial load
updateDashboard();

// Refresh every minute
setInterval(updateDashboard, 60000);

function formatEventType(type) {
    return type
        .split('_')
        .map(word => word.charAt(0).toUpperCase() + word.slice(1))
        .join(' ');
}

function getEventIcon(type) {
    const icons = {
        request: '🔍',
        successful_generation: '✨',
        rate_limit_hit: '⚠️',
        follow: '👤',
        error: '❌'
    };
    return icons[type] || '📝';
}

function getEventColor(type) {
    const colors = {
        request: 'linear-gradient(135deg, #6366f1, #8b5cf6)',
        successful_generation: 'linear-gradient(135deg, #22c55e, #16a34a)',
        rate_limit_hit: 'linear-gradient(135deg, #f59e0b, #d97706)',
        follow: 'linear-gradient(135deg, #06b6d4, #0891b2)',
        error: 'linear-gradient(135deg, #ef4444, #dc2626)'
    };
    return colors[type] || 'linear-gradient(135deg, #6366f1, #8b5cf6)';
}

function formatTimeAgo(timestamp) {
    const seconds = Math.floor((new Date() - new Date(timestamp)) / 1000);

    const intervals = {
        year: 31536000,
        month: 2592000,
        week: 604800,
        day: 86400,
        hour: 3600,
        minute: 60,
        second: 1
    };

    for (const [unit, secondsInUnit] of Object.entries(intervals)) {
        const interval = Math.floor(seconds / secondsInUnit);
        if (interval >= 1) {
            return `${interval} ${unit}${interval === 1 ? '' : 's'} ago`;
        }
    }
    return 'just now';
}

function updateTimeline(data) {
    const timeline = document.getElementById('timeline');
    timeline.innerHTML = ''; // Clear existing events

    // Sort events by timestamp in descending order
    const sortedEvents = [...data].sort((a, b) =>
        new Date(b.Timestamp) - new Date(a.Timestamp));

    // Take only the last 20 events
    const recentEvents = sortedEvents.slice(0, 20);

    recentEvents.forEach(event => {
        const timeAgo = formatTimeAgo(event.Timestamp);
        const formattedType = formatEventType(event.EventType);

        let details = '';
        if (event.Details) {
            if (event.Details.mediaType) {
                details += `Media Type: ${event.Details.mediaType}`;
            }
            if (event.Details.responseTime) {
                details += details ? ' • ' : '';
                details += `Response Time: ${event.Details.responseTime}ms`;
            }
        }

        const itemHTML = `
            <div class="timeline-item">
                <div class="timeline-content">
                    <div class="timeline-time">${timeAgo}</div>
                    <div class="timeline-title">
                        ${getEventIcon(event.EventType)} ${formattedType}
                    </div>
                    ${details ? `<div class="timeline-details">${details}</div>` : ''}
                    <div class="timeline-tag" style="background: ${getEventColor(event.EventType)}">
                        User ID: ${event.UserID.slice(0, 8)}...
                    </div>
                </div>
            </div>
        `;

        timeline.insertAdjacentHTML('beforeend', itemHTML);
    });
}